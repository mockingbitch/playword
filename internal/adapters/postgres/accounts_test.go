package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"playword/internal/adapters/postgres"
	"playword/internal/domain/account"
)

// testStore creates and drops a database per test (see internal/ingest).
func testStore(t *testing.T) *postgres.Store {
	t.Helper()
	admin := os.Getenv("PLAYWORD_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("PLAYWORD_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("playword_it_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "drop database "+name+" with (force)")
		_ = conn.Close(context.Background())
	})
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	st, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx, "up"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestAccountStore(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	u, err := st.CreateUser(ctx, "mc@example.vn", "hash", "MC", false)
	if err != nil || u.DefaultLevel != "L25" || u.Role != "user" {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := st.CreateUser(ctx, "MC@Example.VN", "hash", "", false); !errors.Is(err, account.ErrEmailTaken) {
		t.Errorf("citext uniqueness: %v", err)
	}
	if got, hash, err := st.UserByEmail(ctx, "Mc@example.vn"); err != nil || got.ID != u.ID || hash != "hash" {
		t.Errorf("lookup ignores case: %v", err)
	}
	if _, _, err := st.UserByEmail(ctx, "nobody@example.vn"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}

	var locked time.Time
	for i := 1; i <= account.MaxFailures; i++ {
		n, l, err := st.RecordLoginFailure(ctx, u.ID)
		if err != nil || n != i {
			t.Fatalf("failure %d: %d %v", i, n, err)
		}
		locked = l
	}
	if d := time.Until(locked); d < account.LockFor-time.Minute || d > account.LockFor+time.Minute {
		t.Errorf("locked until %v", locked)
	}
	if err := st.ResetLoginFailures(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.UserByEmail(ctx, "mc@example.vn"); got.FailedLogins != 0 || !got.LockedUntil.IsZero() {
		t.Errorf("reset: %+v", got)
	}

	u.DefaultDialect, u.DefaultLang, u.DefaultLevel, u.DisplayName = "nam", "en", "perfect", ""
	if err := st.UpdateSettings(ctx, u); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.UserByID(ctx, u.ID); got.DefaultLevel != "perfect" || got.DisplayName != "" {
		t.Errorf("settings: %+v", got)
	}
	if err := st.SetRole(ctx, "mc@example.vn", "admin"); err != nil {
		t.Fatal(err)
	}

	if err := st.CreateSession(ctx, "tok-live", u.ID, time.Now().Add(time.Hour), "iphash", "ua"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "tok-old", u.ID, time.Now().Add(-time.Hour), "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _, err := st.SessionUser(ctx, "tok-live"); err != nil || got.Role != "admin" {
		t.Errorf("session: %+v %v", got, err)
	}
	if _, _, err := st.SessionUser(ctx, "tok-old"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("expired session: %v", err)
	}
	if n, err := st.DeleteExpiredSessions(ctx); err != nil || n != 1 {
		t.Errorf("cleanup: %d %v", n, err)
	}

	b := &account.Book{UserID: u.ID, Title: "Verse 1", Description: ""}
	if err := st.CreateBook(ctx, b); err != nil || b.ID == "" {
		t.Fatal(err)
	}
	if _, err := st.Book(ctx, "not-a-uuid"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
	custom := &account.Item{BookID: b.ID, Text: "chạy show", Lang: "vi", MatchedFrom: "ngày tàn"}
	if err := st.AddItem(ctx, custom); err != nil {
		t.Fatal(err)
	}
	second := &account.Item{BookID: b.ID, Text: "tay trắng", Lang: "vi"}
	if err := st.AddItem(ctx, second); err != nil || second.Ord != 1 {
		t.Fatalf("ord %d %v", second.Ord, err)
	}
	if err := st.ReorderItems(ctx, b.ID, []int64{second.ID, custom.ID}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.Items(ctx, b.ID)
	if len(items) != 2 || items[0].ID != second.ID || items[1].MatchedFrom != "ngày tàn" {
		t.Errorf("items = %+v", items)
	}
	if err := st.ReorderItems(ctx, b.ID, []int64{second.ID, 999999}); err == nil {
		t.Error("reorder with a foreign id must fail and roll back")
	}
	if items, _ := st.Items(ctx, b.ID); items[0].ID != second.ID {
		t.Error("failed reorder changed the order")
	}
	if err := st.UpdateItemNote(ctx, b.ID, custom.ID, "hook"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteItem(ctx, b.ID, 424242); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("missing item: %v", err)
	}
	other, _ := st.CreateUser(ctx, "x@example.vn", "h", "", false)
	if err := st.UpdateBook(ctx, &account.Book{ID: b.ID, UserID: other.ID, Title: "x"}); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("foreign update: %v", err)
	}
	if books, _ := st.Books(ctx, u.ID); len(books) != 1 || books[0].ItemCount != 2 {
		t.Errorf("books: %+v", books)
	}

	// Deleting the user cascades to sessions, books and items.
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Book(ctx, b.ID); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("book after user delete: %v", err)
	}
	if _, _, err := st.SessionUser(ctx, "tok-live"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("session after user delete: %v", err)
	}
}

func TestIdentityStore(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	g, err := st.CreateUser(ctx, "g@gmail.com", "", "G", true)
	if err != nil || g.HasPassword {
		t.Fatalf("provider-only user: %+v %v", g, err)
	}
	if got, hash, err := st.UserByEmail(ctx, "g@gmail.com"); err != nil || hash != "" || got.HasPassword {
		t.Errorf("no password: %q %v", hash, err)
	}
	if _, err := st.UserIDByIdentity(ctx, "google", "sub-1"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("unknown identity: %v", err)
	}
	id := account.Identity{Provider: "google", Subject: "sub-1", Email: "g@gmail.com", EmailVerified: true}
	if err := st.LinkIdentity(ctx, g.ID, id); err != nil {
		t.Fatal(err)
	}
	if got, err := st.UserIDByIdentity(ctx, "google", "sub-1"); err != nil || got != g.ID {
		t.Errorf("by identity: %q %v", got, err)
	}
	if ps, err := st.Identities(ctx, g.ID); err != nil || len(ps) != 1 || ps[0] != "google" {
		t.Errorf("identities: %v %v", ps, err)
	}
	if err := st.MarkEmailVerified(ctx, g.ID); err != nil {
		t.Error(err)
	}

	// password set, then cleared (account linked to a provider)
	if err := st.SetPasswordHash(ctx, g.ID, "h2"); err != nil {
		t.Fatal(err)
	}
	if _, hash, _ := st.UserByEmail(ctx, "g@gmail.com"); hash != "h2" {
		t.Errorf("hash = %q", hash)
	}
	if err := st.SetPasswordHash(ctx, g.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, hash, _ := st.UserByEmail(ctx, "g@gmail.com"); hash != "" {
		t.Errorf("cleared hash = %q", hash)
	}

	exp := time.Now().Add(time.Hour)
	for _, tok := range []string{"tok-a", "tok-b", "tok-c"} {
		if err := st.CreateSession(ctx, tok, g.ID, exp, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := st.DeleteUserSessions(ctx, g.ID, "tok-b"); err != nil || n != 2 {
		t.Errorf("delete sessions: %d %v", n, err)
	}
	if _, _, err := st.SessionUser(ctx, "tok-b"); err != nil {
		t.Errorf("kept session: %v", err)
	}
	if n, _ := st.DeleteUserSessions(ctx, g.ID, ""); n != 1 {
		t.Errorf("delete all: %d", n)
	}

	if err := st.DeleteUser(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserIDByIdentity(ctx, "google", "sub-1"); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("identity outlived its user: %v", err)
	}
}
