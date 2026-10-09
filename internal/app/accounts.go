package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"playword/internal/domain/account"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/ports"
)

// sessionCacheTTL bounds how long a revoked session can still be honoured
// by another server (spec addendum §4: "cache in-process 5 phút").
const sessionCacheTTL = 5 * time.Minute

// Accounts implements sign-up, sign-in, settings and rhyme books.
type Accounts struct {
	store  ports.AccountStore
	ipSalt string
	now    func() time.Time

	cache *lru.Cache[string, cachedSession] // token id -> user
	dummy string                            // hash verified for unknown emails, to equalize timing
	once  sync.Once
}

type cachedSession struct {
	user    *account.User
	expires time.Time // min(session expiry, cache TTL)
}

// NewAccounts wires the account use cases; ipSalt keys the IP hashes kept
// with sessions.
func NewAccounts(store ports.AccountStore, ipSalt string) *Accounts {
	c, _ := lru.New[string, cachedSession](10_000)
	return &Accounts{store: store, ipSalt: ipSalt, now: time.Now, cache: c}
}

// Register creates an account and signs it in; it returns the session token.
func (a *Accounts) Register(ctx context.Context, email, password, name, ip, ua string) (*account.User, string, error) {
	email, err := account.NormalizeEmail(email)
	if err != nil {
		return nil, "", err
	}
	if err := account.CheckPassword(password); err != nil {
		return nil, "", err
	}
	if name, err = account.CleanNote(name); err != nil || len([]rune(name)) > 60 {
		return nil, "", fmt.Errorf("%w: display name", account.ErrInvalidInput)
	}
	hash, err := account.HashPassword(password)
	if err != nil {
		return nil, "", err
	}
	u, err := a.store.CreateUser(ctx, email, hash, name, false)
	if err != nil {
		return nil, "", err
	}
	token, err := a.newSession(ctx, u, ip, ua)
	return u, token, err
}

// LoginOAuth signs in with an identity a provider vouched for. It returns
// created=true for a new account. When the verified email already belongs to
// a password account, the identity is linked and — since that address was
// never verified here — the old password is disabled and other sessions end,
// so whoever registered it first cannot keep access (pre-hijacking).
func (a *Accounts) LoginOAuth(ctx context.Context, id account.Identity, ip, ua string) (u *account.User, token string, created bool, err error) {
	if id.Provider == "" || id.Subject == "" {
		return nil, "", false, fmt.Errorf("%w: identity", account.ErrInvalidInput)
	}
	userID, err := a.store.UserIDByIdentity(ctx, id.Provider, id.Subject)
	switch {
	case err == nil:
		if u, err = a.store.UserByID(ctx, userID); err != nil {
			return nil, "", false, err
		}
	case errors.Is(err, account.ErrNotFound):
		if !id.EmailVerified {
			return nil, "", false, account.ErrEmailNotVerified
		}
		email, err := account.NormalizeEmail(id.Email)
		if err != nil {
			return nil, "", false, err
		}
		existing, hash, err := a.store.UserByEmail(ctx, email)
		switch {
		case err == nil:
			u = existing
			if hash != "" {
				if err := a.store.SetPasswordHash(ctx, u.ID, ""); err != nil {
					return nil, "", false, err
				}
				if _, err := a.store.DeleteUserSessions(ctx, u.ID, ""); err != nil {
					return nil, "", false, err
				}
				a.forget(u.ID)
			}
		case errors.Is(err, account.ErrNotFound):
			name := []rune(strings.TrimSpace(id.Name))
			if u, err = a.store.CreateUser(ctx, email, "", string(name[:min(len(name), 60)]), true); err != nil {
				return nil, "", false, err
			}
			created = true
		default:
			return nil, "", false, err
		}
		if err := a.store.LinkIdentity(ctx, u.ID, id); err != nil {
			return nil, "", false, err
		}
		if err := a.store.MarkEmailVerified(ctx, u.ID); err != nil {
			return nil, "", false, err
		}
	default:
		return nil, "", false, err
	}
	if err := a.store.ResetLoginFailures(ctx, u.ID); err != nil {
		return nil, "", false, err
	}
	token, err = a.newSession(ctx, u, ip, ua)
	return u, token, created, err
}

// Profile adds what the settings page shows: whether a password is set and
// which providers are linked.
func (a *Accounts) Profile(ctx context.Context, u *account.User) (*account.User, error) {
	full, _, err := a.store.UserByEmail(ctx, u.Email)
	if err != nil {
		return nil, err
	}
	if full.Providers, err = a.store.Identities(ctx, u.ID); err != nil {
		return nil, err
	}
	p := *u
	p.HasPassword, p.Providers = full.HasPassword, full.Providers
	return &p, nil
}

// SetPassword sets or changes the password; changing one needs the current
// password. Every other session of the user ends.
func (a *Accounts) SetPassword(ctx context.Context, u *account.User, current, next, keepToken string) error {
	_, hash, err := a.store.UserByEmail(ctx, u.Email)
	if err != nil {
		return err
	}
	if hash != "" {
		if ok, err := account.VerifyPassword(current, hash); err != nil || !ok {
			return account.ErrBadCredentials
		}
	}
	if err := account.CheckPassword(next); err != nil {
		return err
	}
	newHash, err := account.HashPassword(next)
	if err != nil {
		return err
	}
	if err := a.store.SetPasswordHash(ctx, u.ID, newHash); err != nil {
		return err
	}
	keep := ""
	if keepToken != "" {
		keep = account.TokenID(keepToken)
	}
	if _, err := a.store.DeleteUserSessions(ctx, u.ID, keep); err != nil {
		return err
	}
	a.forget(u.ID)
	return nil
}

// Login checks a password. Unknown emails cost the same as wrong passwords,
// and 10 consecutive failures lock the account for 15 minutes.
func (a *Accounts) Login(ctx context.Context, email, password, ip, ua string) (*account.User, string, error) {
	email, err := account.NormalizeEmail(email)
	if err != nil {
		return nil, "", account.ErrBadCredentials
	}
	u, hash, err := a.store.UserByEmail(ctx, email)
	if errors.Is(err, account.ErrNotFound) {
		a.once.Do(func() { a.dummy, _ = account.HashPassword("timing-equalizer") })
		_, _ = account.VerifyPassword(password, a.dummy)
		return nil, "", account.ErrBadCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if u.LockedUntil.After(a.now()) {
		return nil, "", account.ErrLocked
	}
	if hash == "" { // provider-only account: no password to match
		a.once.Do(func() { a.dummy, _ = account.HashPassword("timing-equalizer") })
		_, _ = account.VerifyPassword(password, a.dummy)
		return nil, "", account.ErrBadCredentials
	}
	ok, err := account.VerifyPassword(password, hash)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		_, locked, err := a.store.RecordLoginFailure(ctx, u.ID)
		if err != nil {
			return nil, "", err
		}
		if locked.After(a.now()) {
			return nil, "", account.ErrLocked
		}
		return nil, "", account.ErrBadCredentials
	}
	if err := a.store.ResetLoginFailures(ctx, u.ID); err != nil {
		return nil, "", err
	}
	token, err := a.newSession(ctx, u, ip, ua)
	return u, token, err
}

func (a *Accounts) newSession(ctx context.Context, u *account.User, ip, ua string) (string, error) {
	token := account.NewToken()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	err := a.store.CreateSession(ctx, account.TokenID(token), u.ID, a.now().Add(account.SessionTTL), account.HashIP(ip, a.ipSalt), ua)
	return token, err
}

// Authenticate resolves a session cookie to its user.
func (a *Accounts) Authenticate(ctx context.Context, token string) (*account.User, error) {
	if token == "" {
		return nil, account.ErrUnauthenticated
	}
	id := account.TokenID(token)
	if c, ok := a.cache.Get(id); ok && c.expires.After(a.now()) {
		return c.user, nil
	}
	u, expires, err := a.store.SessionUser(ctx, id)
	if errors.Is(err, account.ErrNotFound) {
		a.cache.Remove(id)
		return nil, account.ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	a.cache.Add(id, cachedSession{user: u, expires: earliest(expires, a.now().Add(sessionCacheTTL))})
	return u, nil
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Logout ends a session.
func (a *Accounts) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	id := account.TokenID(token)
	a.cache.Remove(id)
	return a.store.DeleteSession(ctx, id)
}

// forget drops every cached session of a user (settings changed, deleted).
func (a *Accounts) forget(userID string) {
	for _, k := range a.cache.Keys() {
		if c, ok := a.cache.Peek(k); ok && c.user.ID == userID {
			a.cache.Remove(k)
		}
	}
}

// Settings are the user's editable preferences.
type Settings struct {
	DisplayName, Dialect, Lang, Level string
}

// UpdateSettings validates and stores display name and search defaults.
func (a *Accounts) UpdateSettings(ctx context.Context, u *account.User, s Settings) (*account.User, error) {
	name, err := account.CleanNote(s.DisplayName)
	if err != nil || len([]rune(name)) > 60 {
		return nil, fmt.Errorf("%w: display name", account.ErrInvalidInput)
	}
	d, err := rhyme.ParseDialect(s.Dialect)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", account.ErrInvalidInput, err)
	}
	lang := entry.Lang(s.Lang)
	if lang != entry.VI && lang != entry.EN {
		return nil, fmt.Errorf("%w: language", account.ErrInvalidInput)
	}
	l, err := rhyme.ParseLevel(s.Level)
	if err != nil || l.IsEnglish() != (lang == entry.EN) {
		return nil, fmt.Errorf("%w: level %q for language %s", account.ErrInvalidInput, s.Level, lang)
	}
	nu := *u
	nu.DisplayName, nu.DefaultDialect, nu.DefaultLang, nu.DefaultLevel = name, string(d), string(lang), l.String()
	if err := a.store.UpdateSettings(ctx, &nu); err != nil {
		return nil, err
	}
	a.forget(u.ID)
	return &nu, nil
}

// DeleteAccount removes the user, their sessions and books, after
// re-checking the password — or, for an account without one (Google
// sign-in), the email typed again. Analytics events are anonymized (M7).
func (a *Accounts) DeleteAccount(ctx context.Context, u *account.User, password string) error {
	_, hash, err := a.store.UserByEmail(ctx, u.Email)
	if err != nil {
		return err
	}
	if hash == "" {
		if !strings.EqualFold(strings.TrimSpace(password), u.Email) {
			return account.ErrBadCredentials
		}
	} else if ok, err := account.VerifyPassword(password, hash); err != nil || !ok {
		return account.ErrBadCredentials
	}
	if err := a.store.DeleteUser(ctx, u.ID); err != nil {
		return err
	}
	a.forget(u.ID)
	return nil
}

// CreateBook makes a notebook for u.
func (a *Accounts) CreateBook(ctx context.Context, u *account.User, title, desc string, public bool) (*account.Book, error) {
	if u == nil {
		return nil, account.ErrUnauthenticated
	}
	title, err := account.CleanTitle(title)
	if err != nil {
		return nil, err
	}
	if desc, err = account.CleanNote(desc); err != nil {
		return nil, err
	}
	b := &account.Book{UserID: u.ID, Title: title, Description: desc, IsPublic: public}
	return b, a.store.CreateBook(ctx, b)
}

// Books lists u's notebooks, most recently changed first.
func (a *Accounts) Books(ctx context.Context, u *account.User) ([]account.Book, error) {
	if u == nil {
		return nil, account.ErrUnauthenticated
	}
	return a.store.Books(ctx, u.ID)
}

// Book returns a notebook its viewer may read: their own, or a public one.
// Someone else's private book is reported as not found, not forbidden, so
// its existence does not leak.
func (a *Accounts) Book(ctx context.Context, viewer *account.User, id string) (*account.Book, []account.Item, error) {
	b, err := a.store.Book(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !b.IsPublic && (viewer == nil || viewer.ID != b.UserID) {
		return nil, nil, account.ErrNotFound
	}
	items, err := a.store.Items(ctx, id)
	return b, items, err
}

// owned loads a book u may change.
func (a *Accounts) owned(ctx context.Context, u *account.User, id string) (*account.Book, error) {
	if u == nil {
		return nil, account.ErrUnauthenticated
	}
	b, err := a.store.Book(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.UserID != u.ID {
		return nil, account.ErrNotFound
	}
	return b, nil
}

// UpdateBook changes title, description and visibility.
func (a *Accounts) UpdateBook(ctx context.Context, u *account.User, id, title, desc string, public bool) (*account.Book, error) {
	b, err := a.owned(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if b.Title, err = account.CleanTitle(title); err != nil {
		return nil, err
	}
	if b.Description, err = account.CleanNote(desc); err != nil {
		return nil, err
	}
	b.IsPublic = public
	return b, a.store.UpdateBook(ctx, b)
}

// DeleteBook removes a notebook and its items.
func (a *Accounts) DeleteBook(ctx context.Context, u *account.User, id string) error {
	if _, err := a.owned(ctx, u, id); err != nil {
		return err
	}
	return a.store.DeleteBook(ctx, id, u.ID)
}

// NewItem is what a client may add: an entry (EntryID) or custom text.
type NewItem struct {
	EntryID     int64
	Text        string
	Note        string
	Lang        string
	Dialect     string
	Level       string
	MatchedFrom string
}

// AddItem saves a rhyme into a book; an entry already in it is not added
// twice.
func (a *Accounts) AddItem(ctx context.Context, u *account.User, bookID string, n NewItem) (*account.Item, error) {
	if _, err := a.owned(ctx, u, bookID); err != nil {
		return nil, err
	}
	it, err := a.cleanItem(ctx, bookID, n)
	if err != nil {
		return nil, err
	}
	if it.EntryID != 0 {
		dup, err := a.store.HasEntryItem(ctx, bookID, it.EntryID)
		if err != nil || dup {
			return it, err
		}
	}
	return it, a.store.AddItem(ctx, it)
}

func (a *Accounts) cleanItem(ctx context.Context, bookID string, n NewItem) (*account.Item, error) {
	it := &account.Item{BookID: bookID, EntryID: n.EntryID, Lang: n.Lang, Dialect: n.Dialect, Level: n.Level}
	var err error
	if it.Note, err = account.CleanNote(n.Note); err != nil {
		return nil, err
	}
	if it.MatchedFrom, err = account.CleanNote(n.MatchedFrom); err != nil {
		return nil, err
	}
	if it.Lang == "" {
		it.Lang = string(entry.VI)
	}
	if it.Lang != string(entry.VI) && it.Lang != string(entry.EN) {
		return nil, fmt.Errorf("%w: language", account.ErrInvalidInput)
	}
	if it.EntryID != 0 {
		ok, err := a.store.EntryExists(ctx, it.EntryID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: unknown entry %d", account.ErrInvalidInput, it.EntryID)
		}
		return it, nil
	}
	if it.Text, err = account.CleanNote(n.Text); err != nil || it.Text == "" {
		return nil, fmt.Errorf("%w: text or entry_id is required", account.ErrInvalidInput)
	}
	return it, nil
}

// UpdateItemNote edits an item's note.
func (a *Accounts) UpdateItemNote(ctx context.Context, u *account.User, bookID string, itemID int64, note string) error {
	if _, err := a.owned(ctx, u, bookID); err != nil {
		return err
	}
	note, err := account.CleanNote(note)
	if err != nil {
		return err
	}
	return a.store.UpdateItemNote(ctx, bookID, itemID, note)
}

// DeleteItem removes an item.
func (a *Accounts) DeleteItem(ctx context.Context, u *account.User, bookID string, itemID int64) error {
	if _, err := a.owned(ctx, u, bookID); err != nil {
		return err
	}
	return a.store.DeleteItem(ctx, bookID, itemID)
}

// Reorder sets the order of a book's items; ids must be exactly its items.
func (a *Accounts) Reorder(ctx context.Context, u *account.User, bookID string, ids []int64) error {
	if _, err := a.owned(ctx, u, bookID); err != nil {
		return err
	}
	items, err := a.store.Items(ctx, bookID)
	if err != nil {
		return err
	}
	have := make([]int64, len(items))
	for i, it := range items {
		have[i] = it.ID
	}
	got := slices.Clone(ids)
	slices.Sort(have)
	slices.Sort(got)
	if !slices.Equal(have, got) {
		return fmt.Errorf("%w: order must list every item of the book exactly once", account.ErrInvalidInput)
	}
	return a.store.ReorderItems(ctx, bookID, ids)
}

// Export renders a book the viewer may read as txt, md or json.
func (a *Accounts) Export(ctx context.Context, viewer *account.User, bookID, format string) (data []byte, contentType, filename string, err error) {
	b, items, err := a.Book(ctx, viewer, bookID)
	if err != nil {
		return nil, "", "", err
	}
	name := safeFilename(b.Title)
	var buf bytes.Buffer
	switch format {
	case "txt", "":
		fmt.Fprintf(&buf, "%s\n", b.Title)
		for _, it := range items {
			fmt.Fprintf(&buf, "%s", it.Text)
			if it.Note != "" {
				fmt.Fprintf(&buf, " — %s", it.Note)
			}
			buf.WriteByte('\n')
		}
		return buf.Bytes(), "text/plain; charset=utf-8", name + ".txt", nil
	case "md":
		fmt.Fprintf(&buf, "# %s\n\n", b.Title)
		if b.Description != "" {
			fmt.Fprintf(&buf, "%s\n\n", b.Description)
		}
		for _, it := range items {
			fmt.Fprintf(&buf, "- **%s**", it.Text)
			if it.MatchedFrom != "" {
				fmt.Fprintf(&buf, " (vần với “%s”)", it.MatchedFrom)
			}
			if it.Note != "" {
				fmt.Fprintf(&buf, " — %s", it.Note)
			}
			buf.WriteByte('\n')
		}
		return buf.Bytes(), "text/markdown; charset=utf-8", name + ".md", nil
	case "json":
		type item struct {
			Text        string `json:"text"`
			EntryID     int64  `json:"entry_id,omitempty"`
			Note        string `json:"note,omitempty"`
			Lang        string `json:"lang"`
			Dialect     string `json:"dialect,omitempty"`
			Level       string `json:"level,omitempty"`
			MatchedFrom string `json:"matched_from,omitempty"`
		}
		out := struct {
			Title       string `json:"title"`
			Description string `json:"description,omitempty"`
			Items       []item `json:"items"`
		}{Title: b.Title, Description: b.Description, Items: []item{}}
		for _, it := range items {
			out.Items = append(out.Items, item{it.Text, it.EntryID, it.Note, it.Lang, it.Dialect, it.Level, it.MatchedFrom})
		}
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		err := enc.Encode(out)
		return buf.Bytes(), "application/json", name + ".json", err
	}
	return nil, "", "", fmt.Errorf("%w: format must be txt, md or json", account.ErrInvalidInput)
}

func safeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, s)
	if s == "" {
		return "so-van"
	}
	return s
}

// ImportGuest turns a guest's cookie notebook into a real book on sign-up.
func (a *Accounts) ImportGuest(ctx context.Context, u *account.User, items []account.GuestItem) (*account.Book, error) {
	if len(items) == 0 {
		return nil, nil
	}
	b, err := a.CreateBook(ctx, u, "Sổ tạm", "Lưu khi chưa đăng nhập", false)
	if err != nil {
		return nil, err
	}
	for _, g := range items[:min(len(items), account.GuestMaxItems)] {
		n := NewItem{EntryID: g.EntryID, Text: g.Text, Lang: g.Lang, Dialect: g.Dialect, Level: g.Level, MatchedFrom: g.MatchedFrom}
		if _, err := a.AddItem(ctx, u, b.ID, n); err != nil {
			// an entry that vanished keeps its text as a custom item
			n.EntryID = 0
			if _, err := a.AddItem(ctx, u, b.ID, n); err != nil {
				return b, err
			}
		}
	}
	return b, nil
}
