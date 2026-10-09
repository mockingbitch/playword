package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"playword/internal/app/apptest"
	"playword/internal/domain/account"
)

var ctx = context.Background()

func newAccounts(t *testing.T) (*Accounts, *apptest.Accounts, *time.Time) {
	t.Helper()
	store := apptest.NewAccounts()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	a := NewAccounts(store, "salt")
	a.now = store.Now
	return a, store, &now
}

func register(t *testing.T, a *Accounts, email string) (*account.User, string) {
	t.Helper()
	u, token, err := a.Register(ctx, email, "correct horse battery", "", "1.2.3.4", "test")
	if err != nil {
		t.Fatal(err)
	}
	return u, token
}

func TestRegisterAndLogin(t *testing.T) {
	a, _, _ := newAccounts(t)
	bad := []struct{ email, pw string }{
		{"not-an-email", "correct horse battery"},
		{"a@b.vn", "short"},
		{"Name <a@b.vn>", "correct horse battery"},
	}
	for _, b := range bad {
		if _, _, err := a.Register(ctx, b.email, b.pw, "", "", ""); !errors.Is(err, account.ErrInvalidInput) {
			t.Errorf("%v: %v", b, err)
		}
	}
	u, token := register(t, a, " Rapper@Example.VN ")
	if u.Email != "rapper@example.vn" || token == "" {
		t.Fatalf("%+v %q", u, token)
	}
	if _, _, err := a.Register(ctx, "rapper@example.vn", "correct horse battery", "", "", ""); !errors.Is(err, account.ErrEmailTaken) {
		t.Errorf("duplicate: %v", err)
	}
	if got, err := a.Authenticate(ctx, token); err != nil || got.ID != u.ID {
		t.Errorf("authenticate: %v", err)
	}
	if _, tok2, err := a.Login(ctx, "RAPPER@example.vn", "correct horse battery", "", ""); err != nil || tok2 == token {
		t.Errorf("login: %v", err)
	}
	for _, c := range []struct{ email, pw string }{{"rapper@example.vn", "wrong password!"}, {"nobody@example.vn", "x"}, {"bad", "x"}} {
		if _, _, err := a.Login(ctx, c.email, c.pw, "", ""); !errors.Is(err, account.ErrBadCredentials) {
			t.Errorf("%v: %v", c, err)
		}
	}
	if err := a.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("after logout: %v", err)
	}
	if _, err := a.Authenticate(ctx, ""); !errors.Is(err, account.ErrUnauthenticated) {
		t.Error("empty token")
	}
	if a.Logout(ctx, "") != nil {
		t.Error("logout without a session is fine")
	}
}

func TestLockout(t *testing.T) {
	a, _, now := newAccounts(t)
	register(t, a, "r@example.vn")
	for i := 1; i < account.MaxFailures; i++ {
		if _, _, err := a.Login(ctx, "r@example.vn", "wrong password!", "", ""); !errors.Is(err, account.ErrBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, err := a.Login(ctx, "r@example.vn", "wrong password!", "", ""); !errors.Is(err, account.ErrLocked) {
		t.Fatalf("10th failure must lock: %v", err)
	}
	if _, _, err := a.Login(ctx, "r@example.vn", "correct horse battery", "", ""); !errors.Is(err, account.ErrLocked) {
		t.Fatalf("even the right password is refused while locked: %v", err)
	}
	*now = now.Add(account.LockFor + time.Second)
	if _, _, err := a.Login(ctx, "r@example.vn", "correct horse battery", "", ""); err != nil {
		t.Fatalf("after the lock: %v", err)
	}
	// success resets the counter
	if _, _, err := a.Login(ctx, "r@example.vn", "wrong password!", "", ""); !errors.Is(err, account.ErrBadCredentials) {
		t.Errorf("counter not reset: %v", err)
	}
}

func TestSessionCacheAndExpiry(t *testing.T) {
	a, store, now := newAccounts(t)
	_, token := register(t, a, "r@example.vn")
	if _, err := a.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	store.ExpireSessions() // e.g. revoked by another server
	if _, err := a.Authenticate(ctx, token); err != nil {
		t.Error("within 5 minutes the cached session is still honoured")
	}
	*now = now.Add(sessionCacheTTL + time.Second)
	if _, err := a.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("expired session: %v", err)
	}
}

func TestSettingsAndDelete(t *testing.T) {
	a, _, _ := newAccounts(t)
	u, token := register(t, a, "r@example.vn")
	nu, err := a.UpdateSettings(ctx, u, Settings{DisplayName: "MC", Dialect: "nam", Lang: "en", Level: "perfect"})
	if err != nil || nu.DefaultLevel != "perfect" || nu.DisplayName != "MC" {
		t.Fatalf("%+v %v", nu, err)
	}
	if got, _ := a.Authenticate(ctx, token); got.DefaultDialect != "nam" {
		t.Error("settings must reach the cached session")
	}
	for _, s := range []Settings{
		{Dialect: "hue", Lang: "vi", Level: "L25"},
		{Dialect: "bac", Lang: "fr", Level: "L25"},
		{Dialect: "bac", Lang: "vi", Level: "perfect"},
		{Dialect: "bac", Lang: "vi", Level: "L25", DisplayName: strings.Repeat("x", 61)},
	} {
		if _, err := a.UpdateSettings(ctx, u, s); !errors.Is(err, account.ErrInvalidInput) {
			t.Errorf("%+v: %v", s, err)
		}
	}
	if err := a.DeleteAccount(ctx, u, "wrong password!"); !errors.Is(err, account.ErrBadCredentials) {
		t.Errorf("delete needs the password: %v", err)
	}
	if err := a.DeleteAccount(ctx, u, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("sessions die with the account: %v", err)
	}
}

func TestBooks(t *testing.T) {
	a, _, _ := newAccounts(t)
	owner, _ := register(t, a, "owner@example.vn")
	other, _ := register(t, a, "other@example.vn")

	if _, err := a.CreateBook(ctx, nil, "x", "", false); !errors.Is(err, account.ErrUnauthenticated) {
		t.Error("guests cannot create books")
	}
	if _, err := a.CreateBook(ctx, owner, "  ", "", false); !errors.Is(err, account.ErrInvalidInput) {
		t.Error("empty title")
	}
	b, err := a.CreateBook(ctx, owner, "Verse 1", "đêm nay", false)
	if err != nil {
		t.Fatal(err)
	}

	// Private: only the owner sees it; others get "not found", not "forbidden".
	for _, viewer := range []*account.User{nil, other} {
		if _, _, err := a.Book(ctx, viewer, b.ID); !errors.Is(err, account.ErrNotFound) {
			t.Errorf("viewer %v: %v", viewer, err)
		}
	}
	if _, err := a.AddItem(ctx, other, b.ID, NewItem{EntryID: 1}); !errors.Is(err, account.ErrNotFound) {
		t.Error("only the owner adds items")
	}

	first, err := a.AddItem(ctx, owner, b.ID, NewItem{EntryID: 1, Lang: "vi", MatchedFrom: "ngày tàn", Level: "L25"})
	if err != nil {
		t.Fatal(err)
	}
	if dup, _ := a.AddItem(ctx, owner, b.ID, NewItem{EntryID: 1}); dup.ID != 0 {
		t.Error("the same entry is not saved twice")
	}
	custom, err := a.AddItem(ctx, owner, b.ID, NewItem{Text: "chạy show"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []NewItem{{EntryID: 999}, {}, {Text: "x", Lang: "fr"}, {Text: strings.Repeat("x", 1001)}} {
		if _, err := a.AddItem(ctx, owner, b.ID, n); !errors.Is(err, account.ErrInvalidInput) {
			t.Errorf("%+v: %v", n, err)
		}
	}
	if err := a.UpdateItemNote(ctx, owner, b.ID, custom.ID, "hook"); err != nil {
		t.Fatal(err)
	}

	// Order is deterministic and must name every item exactly once.
	if err := a.Reorder(ctx, owner, b.ID, []int64{custom.ID, first.ID}); err != nil {
		t.Fatal(err)
	}
	_, items, _ := a.Book(ctx, owner, b.ID)
	if len(items) != 2 || items[0].ID != custom.ID || items[1].ID != first.ID {
		t.Errorf("order = %+v", items)
	}
	for _, ids := range [][]int64{{custom.ID}, {custom.ID, custom.ID}, {custom.ID, first.ID, 999}} {
		if err := a.Reorder(ctx, owner, b.ID, ids); !errors.Is(err, account.ErrInvalidInput) {
			t.Errorf("%v: %v", ids, err)
		}
	}

	// Public books are readable by anyone, including signed-out visitors.
	if _, err := a.UpdateBook(ctx, other, b.ID, "hijack", "", true); !errors.Is(err, account.ErrNotFound) {
		t.Error("only the owner edits")
	}
	if _, err := a.UpdateBook(ctx, owner, b.ID, "Verse 1", "đêm nay", true); err != nil {
		t.Fatal(err)
	}
	if _, items, err := a.Book(ctx, nil, b.ID); err != nil || len(items) != 2 {
		t.Errorf("public read: %v", err)
	}

	data, ctype, name, err := a.Export(ctx, nil, b.ID, "md")
	if err != nil || !strings.Contains(string(data), "- **chạy show** — hook") || ctype != "text/markdown; charset=utf-8" || name != "Verse 1.md" {
		t.Errorf("md export: %q %s %s %v", data, ctype, name, err)
	}
	data, _, _, _ = a.Export(ctx, owner, b.ID, "json")
	var parsed struct {
		Title string
		Items []struct{ Text, MatchedFrom string }
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Title != "Verse 1" || len(parsed.Items) != 2 {
		t.Errorf("json export: %s %v", data, err)
	}
	if data, _, name, _ := a.Export(ctx, owner, b.ID, ""); !strings.HasPrefix(string(data), "Verse 1\n") || name != "Verse 1.txt" {
		t.Errorf("txt export: %q", data)
	}
	if _, _, _, err := a.Export(ctx, owner, b.ID, "pdf"); !errors.Is(err, account.ErrInvalidInput) {
		t.Error("unknown format")
	}

	if err := a.DeleteItem(ctx, owner, b.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteBook(ctx, other, b.ID); !errors.Is(err, account.ErrNotFound) {
		t.Error("only the owner deletes")
	}
	if err := a.DeleteBook(ctx, owner, b.ID); err != nil {
		t.Fatal(err)
	}
	if books, _ := a.Books(ctx, owner); len(books) != 0 {
		t.Error("deleted")
	}
	if safeFilename(`a/b:c`) != "a-b-c" || safeFilename("") != "so-van" {
		t.Error("safeFilename")
	}
}

func TestImportGuest(t *testing.T) {
	a, _, _ := newAccounts(t)
	u, _ := register(t, a, "r@example.vn")
	if b, err := a.ImportGuest(ctx, u, nil); b != nil || err != nil {
		t.Error("nothing to import")
	}
	b, err := a.ImportGuest(ctx, u, []account.GuestItem{
		{Text: "may mắn", EntryID: 3, MatchedFrom: "ngày tàn"},
		{Text: "đã biến mất", EntryID: 999}, // entry gone: kept as text
	})
	if err != nil {
		t.Fatal(err)
	}
	_, items, _ := a.Book(ctx, u, b.ID)
	if len(items) != 2 || items[1].EntryID != 0 || items[1].Text != "đã biến mất" {
		t.Errorf("items = %+v", items)
	}
}
