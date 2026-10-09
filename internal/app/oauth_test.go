package app

import (
	"errors"
	"testing"

	"playword/internal/domain/account"
)

func googleID(sub, email string) account.Identity {
	return account.Identity{Provider: "google", Subject: sub, Email: email, EmailVerified: true, Name: "MC Google"}
}

func TestLoginOAuthNewAndReturning(t *testing.T) {
	a, _, _ := newAccounts(t)
	u, token, created, err := a.LoginOAuth(ctx, googleID("g1", "MC@Gmail.com"), "1.2.3.4", "test")
	if err != nil || !created || token == "" || u.Email != "mc@gmail.com" || u.DisplayName != "MC Google" {
		t.Fatalf("%+v %v %v", u, created, err)
	}
	if got, err := a.Authenticate(ctx, token); err != nil || got.ID != u.ID {
		t.Errorf("authenticate: %v", err)
	}
	p, err := a.Profile(ctx, u)
	if err != nil || p.HasPassword || len(p.Providers) != 1 || p.Providers[0] != "google" {
		t.Errorf("profile %+v %v", p, err)
	}
	// a Google-only account cannot sign in with any password
	if _, _, err := a.Login(ctx, "mc@gmail.com", "", "", ""); !errors.Is(err, account.ErrBadCredentials) {
		t.Errorf("password login: %v", err)
	}
	// the same Google account again: same user, even after the email changed there
	u2, _, created, err := a.LoginOAuth(ctx, googleID("g1", "renamed@gmail.com"), "", "")
	if err != nil || created || u2.ID != u.ID {
		t.Errorf("returning: %+v %v %v", u2, created, err)
	}
}

func TestLoginOAuthRejects(t *testing.T) {
	a, _, _ := newAccounts(t)
	unverified := googleID("g2", "x@gmail.com")
	unverified.EmailVerified = false
	if _, _, _, err := a.LoginOAuth(ctx, unverified, "", ""); !errors.Is(err, account.ErrEmailNotVerified) {
		t.Errorf("unverified: %v", err)
	}
	if _, _, _, err := a.LoginOAuth(ctx, account.Identity{Provider: "google"}, "", ""); !errors.Is(err, account.ErrInvalidInput) {
		t.Errorf("no subject: %v", err)
	}
	if _, _, _, err := a.LoginOAuth(ctx, googleID("g3", "not an email"), "", ""); err == nil {
		t.Error("bad email accepted")
	}
}

// Someone registered the victim's address with a password before the victim
// ever came. When the victim signs in with Google, the squatter loses access.
func TestLoginOAuthLinksAndEvictsPreHijacker(t *testing.T) {
	a, store, _ := newAccounts(t)
	squatter, squatterToken := register(t, a, "victim@gmail.com")
	u, _, created, err := a.LoginOAuth(ctx, googleID("g9", "victim@gmail.com"), "", "")
	if err != nil || created || u.ID != squatter.ID {
		t.Fatalf("link: %+v %v %v", u, created, err)
	}
	if _, err := a.Authenticate(ctx, squatterToken); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("old session survived: %v", err)
	}
	if _, _, err := a.Login(ctx, "victim@gmail.com", "correct horse battery", "", ""); !errors.Is(err, account.ErrBadCredentials) {
		t.Errorf("old password still works: %v", err)
	}
	if n := store.Sessions(u.ID); n != 1 {
		t.Errorf("sessions = %d, want only the Google one", n)
	}
}

func TestSetPasswordAndDeleteWithoutPassword(t *testing.T) {
	a, store, _ := newAccounts(t)
	u, token, _, err := a.LoginOAuth(ctx, googleID("g1", "mc@gmail.com"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, other, _, _ := a.LoginOAuth(ctx, googleID("g1", "mc@gmail.com"), "", "")
	if err := a.SetPassword(ctx, u, "", "short", token); !errors.Is(err, account.ErrPasswordTooShort) {
		t.Errorf("short: %v", err)
	}
	if err := a.SetPassword(ctx, u, "", "a brand new password", token); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, token); err != nil {
		t.Error("the current session must stay")
	}
	if _, err := a.Authenticate(ctx, other); err == nil || store.Sessions(u.ID) != 1 {
		t.Error("other sessions must end")
	}
	if _, _, err := a.Login(ctx, "mc@gmail.com", "a brand new password", "", ""); err != nil {
		t.Errorf("login with the new password: %v", err)
	}
	if err := a.SetPassword(ctx, u, "wrong current pw", "another new password", token); !errors.Is(err, account.ErrBadCredentials) {
		t.Errorf("change needs the current password: %v", err)
	}
	if err := a.SetPassword(ctx, u, "a brand new password", "another new password", token); err != nil {
		t.Errorf("change: %v", err)
	}

	g, _, _, _ := a.LoginOAuth(ctx, googleID("g2", "only@gmail.com"), "", "")
	if err := a.DeleteAccount(ctx, g, "someone@else.vn"); !errors.Is(err, account.ErrBadCredentials) {
		t.Errorf("wrong email: %v", err)
	}
	if err := a.DeleteAccount(ctx, g, " ONLY@gmail.com "); err != nil {
		t.Errorf("delete by email: %v", err)
	}
}
