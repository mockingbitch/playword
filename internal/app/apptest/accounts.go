// Package apptest has in-memory implementations of the app's ports for
// tests of the app and HTTP layers. The PostgreSQL adapter has its own
// integration tests against the same behaviour.
package apptest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"playword/internal/domain/account"
	"playword/internal/ports"
)

// Accounts is an in-memory ports.AccountStore.
type Accounts struct {
	mu       sync.Mutex
	users    map[string]*account.User
	hashes   map[string]string // user id -> password hash
	idents   map[string]string // provider|subject -> user id
	sessions map[string]session
	books    map[string]*account.Book
	items    map[string][]account.Item // book id -> items
	nextID   int64
	Entries  map[int64]bool // entry ids that exist
	Now      func() time.Time
}

type session struct {
	userID  string
	expires time.Time
}

var _ ports.AccountStore = (*Accounts)(nil)

// NewAccounts returns an empty store where entries 1–100 exist.
func NewAccounts() *Accounts {
	a := &Accounts{users: map[string]*account.User{}, hashes: map[string]string{}, idents: map[string]string{}, sessions: map[string]session{},
		books: map[string]*account.Book{}, items: map[string][]account.Item{}, Entries: map[int64]bool{}, Now: time.Now}
	for i := int64(1); i <= 100; i++ {
		a.Entries[i] = true
	}
	return a
}

func (a *Accounts) id(prefix string) string {
	a.nextID++
	return fmt.Sprintf("%s-%08d-0000-0000-0000-000000000000", prefix, a.nextID)[:36]
}

func (a *Accounts) CreateUser(_ context.Context, email, hash, name string, _ bool) (*account.User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if u.Email == email {
			return nil, account.ErrEmailTaken
		}
	}
	u := &account.User{ID: a.id("u"), Email: email, DisplayName: name, DefaultDialect: "bac", DefaultLang: "vi",
		DefaultLevel: "L25", Role: "user", CreatedAt: a.Now(), HasPassword: hash != ""}
	a.users[u.ID], a.hashes[u.ID] = u, hash
	c := *u
	return &c, nil
}

func (a *Accounts) UserByEmail(_ context.Context, email string) (*account.User, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if u.Email == email {
			c := *u
			c.HasPassword = a.hashes[u.ID] != ""
			return &c, a.hashes[u.ID], nil
		}
	}
	return nil, "", account.ErrNotFound
}

func (a *Accounts) SetPasswordHash(_ context.Context, id, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hashes[id] = hash
	return nil
}

func (a *Accounts) MarkEmailVerified(context.Context, string) error { return nil }

func (a *Accounts) UserIDByIdentity(_ context.Context, provider, subject string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, ok := a.idents[provider+"|"+subject]
	if !ok {
		return "", account.ErrNotFound
	}
	return id, nil
}

func (a *Accounts) LinkIdentity(_ context.Context, userID string, id account.Identity) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.idents[id.Provider+"|"+id.Subject] = userID
	return nil
}

func (a *Accounts) Identities(_ context.Context, userID string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for k, v := range a.idents {
		if v == userID {
			p, _, _ := strings.Cut(k, "|")
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (a *Accounts) DeleteUserSessions(_ context.Context, userID, keep string) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var n int64
	for k, s := range a.sessions {
		if s.userID == userID && k != keep {
			delete(a.sessions, k)
			n++
		}
	}
	return n, nil
}

// Sessions counts a user's sessions (assertions).
func (a *Accounts) Sessions(userID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, s := range a.sessions {
		if s.userID == userID {
			n++
		}
	}
	return n
}

func (a *Accounts) UserByID(_ context.Context, id string) (*account.User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return nil, account.ErrNotFound
	}
	c := *u
	return &c, nil
}

func (a *Accounts) RecordLoginFailure(_ context.Context, id string) (int, time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.users[id]
	u.FailedLogins++
	if u.FailedLogins >= account.MaxFailures {
		u.LockedUntil = a.Now().Add(account.LockFor)
	}
	return u.FailedLogins, u.LockedUntil, nil
}

func (a *Accounts) ResetLoginFailures(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.users[id].FailedLogins, a.users[id].LockedUntil = 0, time.Time{}
	return nil
}

func (a *Accounts) UpdateSettings(_ context.Context, u *account.User) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.users[u.ID]
	cur.DisplayName, cur.DefaultDialect, cur.DefaultLang, cur.DefaultLevel = u.DisplayName, u.DefaultDialect, u.DefaultLang, u.DefaultLevel
	return nil
}

// SetRole makes a user admin (tests of admin pages).
func (a *Accounts) SetRole(email, role string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if u.Email == email {
			u.Role = role
		}
	}
}

func (a *Accounts) DeleteUser(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.users, id)
	for k, s := range a.sessions {
		if s.userID == id {
			delete(a.sessions, k)
		}
	}
	for k, b := range a.books {
		if b.UserID == id {
			delete(a.books, k)
			delete(a.items, k)
		}
	}
	return nil
}

func (a *Accounts) CreateSession(_ context.Context, tokenID, userID string, expires time.Time, _, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[tokenID] = session{userID, expires}
	return nil
}

func (a *Accounts) SessionUser(_ context.Context, tokenID string) (*account.User, time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[tokenID]
	if !ok || !s.expires.After(a.Now()) {
		return nil, time.Time{}, account.ErrNotFound
	}
	c := *a.users[s.userID]
	return &c, s.expires, nil
}

// ExpireSessions moves every session's expiry into the past.
func (a *Accounts) ExpireSessions() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, s := range a.sessions {
		s.expires = a.Now().Add(-time.Second)
		a.sessions[k] = s
	}
}

func (a *Accounts) DeleteSession(_ context.Context, tokenID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, tokenID)
	return nil
}

func (a *Accounts) CreateBook(_ context.Context, b *account.Book) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b.ID, b.CreatedAt, b.UpdatedAt = a.id("b"), a.Now(), a.Now()
	c := *b
	a.books[b.ID] = &c
	return nil
}

func (a *Accounts) Books(_ context.Context, userID string) ([]account.Book, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []account.Book
	for _, b := range a.books {
		if b.UserID == userID {
			c := *b
			c.ItemCount, c.Lyrics = len(a.items[b.ID]), ""
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(x, y account.Book) int { return y.UpdatedAt.Compare(x.UpdatedAt) })
	return out, nil
}

func (a *Accounts) Book(_ context.Context, id string) (*account.Book, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.books[id]
	if !ok {
		return nil, account.ErrNotFound
	}
	c := *b
	c.ItemCount = len(a.items[id])
	return &c, nil
}

func (a *Accounts) UpdateBook(_ context.Context, b *account.Book) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur, ok := a.books[b.ID]
	if !ok || cur.UserID != b.UserID {
		return account.ErrNotFound
	}
	cur.Title, cur.Description, cur.IsPublic, cur.UpdatedAt = b.Title, b.Description, b.IsPublic, a.Now()
	return nil
}

func (a *Accounts) SetLyrics(_ context.Context, id, userID, lyrics string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.books[id]
	if !ok || b.UserID != userID {
		return account.ErrNotFound
	}
	b.Lyrics, b.UpdatedAt = lyrics, a.Now()
	return nil
}

func (a *Accounts) DeleteBook(_ context.Context, id, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b, ok := a.books[id]; !ok || b.UserID != userID {
		return account.ErrNotFound
	}
	delete(a.books, id)
	delete(a.items, id)
	return nil
}

func (a *Accounts) Items(_ context.Context, bookID string) ([]account.Item, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := slices.Clone(a.items[bookID])
	slices.SortStableFunc(out, func(x, y account.Item) int {
		if x.Ord != y.Ord {
			return x.Ord - y.Ord
		}
		return int(x.ID - y.ID)
	})
	return out, nil
}

func (a *Accounts) AddItem(_ context.Context, it *account.Item) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	it.ID, it.CreatedAt = a.nextID, a.Now()
	it.Ord = 0
	for _, x := range a.items[it.BookID] {
		it.Ord = max(it.Ord, x.Ord+1)
	}
	if it.EntryID != 0 {
		it.Text = fmt.Sprintf("entry-%d", it.EntryID)
	}
	a.items[it.BookID] = append(a.items[it.BookID], *it)
	a.books[it.BookID].UpdatedAt = a.Now()
	return nil
}

func (a *Accounts) HasEntryItem(_ context.Context, bookID string, entryID int64) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, x := range a.items[bookID] {
		if x.EntryID == entryID {
			return true, nil
		}
	}
	return false, nil
}

func (a *Accounts) edit(bookID string, itemID int64, f func(*account.Item)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.items[bookID] {
		if a.items[bookID][i].ID == itemID {
			f(&a.items[bookID][i])
			return nil
		}
	}
	return account.ErrNotFound
}

func (a *Accounts) UpdateItemNote(_ context.Context, bookID string, itemID int64, note string) error {
	return a.edit(bookID, itemID, func(it *account.Item) { it.Note = note })
}

func (a *Accounts) DeleteItem(_ context.Context, bookID string, itemID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := a.items[bookID]
	for i := range items {
		if items[i].ID == itemID {
			a.items[bookID] = slices.Delete(items, i, i+1)
			return nil
		}
	}
	return account.ErrNotFound
}

func (a *Accounts) ReorderItems(_ context.Context, bookID string, ids []int64) error {
	for i, id := range ids {
		if err := a.edit(bookID, id, func(it *account.Item) { it.Ord = i }); err != nil {
			return err
		}
	}
	return nil
}

func (a *Accounts) EntryExists(_ context.Context, id int64) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Entries[id], nil
}
