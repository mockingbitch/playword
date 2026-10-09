package httpadapter

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"playword/internal/domain/account"
	"playword/internal/ports"
)

// fakeProvider approves code "ok" for the verifier it was started with.
type fakeProvider struct {
	verifier string
	id       account.Identity
}

func (f *fakeProvider) Name() string { return "google" }
func (f *fakeProvider) AuthURL(state, verifier string) string {
	f.verifier = verifier
	return "https://accounts.example/auth?state=" + url.QueryEscape(state)
}
func (f *fakeProvider) Exchange(_ context.Context, code, verifier string) (account.Identity, error) {
	if code != "ok" || verifier != f.verifier {
		return account.Identity{}, errors.New("invalid_grant")
	}
	return f.id, nil
}

// start follows /auth/google and returns the state sent to the provider.
func start(t *testing.T, b *browser, next string) string {
	t.Helper()
	resp, _ := b.do("GET", "/auth/google?next="+url.QueryEscape(next), "", nil)
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || loc.Host != "accounts.example" {
		t.Fatalf("start: %d %s", resp.StatusCode, loc)
	}
	return loc.Query().Get("state")
}

func TestGoogleSignIn(t *testing.T) {
	p := &fakeProvider{id: account.Identity{Provider: "google", Subject: "g1", Email: "mc@gmail.com", EmailVerified: true, Name: "MC"}}
	ts, _, _ := testServerWith(t, Options{OAuth: map[string]ports.OAuthProvider{"google": p}})
	b := newBrowser(t, ts)

	if _, body := b.do("GET", "/login", "", nil); !strings.Contains(body, `href="/auth/google"`) || !strings.Contains(body, "Tiếp tục với Google") {
		t.Error("login page has no Google button")
	}
	if resp, _ := b.do("GET", "/auth/github", "", nil); resp.StatusCode != 404 {
		t.Errorf("unknown provider: %d", resp.StatusCode)
	}

	// login CSRF: a callback whose state is not this browser's is refused
	state := start(t, b, "/books")
	if resp, _ := b.do("GET", "/auth/google/callback?code=ok&state=forged", "", nil); resp.StatusCode != 400 {
		t.Errorf("forged state: %d", resp.StatusCode)
	}
	// the state cookie is single use, even after a failure
	if resp, _ := b.do("GET", "/auth/google/callback?code=ok&state="+url.QueryEscape(state), "", nil); resp.StatusCode != 400 {
		t.Errorf("reused state: %d", resp.StatusCode)
	}
	other := newBrowser(t, ts)
	if resp, _ := other.do("GET", "/auth/google/callback?code=ok&state="+url.QueryEscape(state), "", nil); resp.StatusCode != 400 {
		t.Errorf("state without its cookie: %d", resp.StatusCode)
	}

	state = start(t, b, "/books")
	if resp, body := b.do("GET", "/auth/google/callback?error=access_denied&state="+url.QueryEscape(state), "", nil); resp.StatusCode != 401 || !strings.Contains(body, "huỷ đăng nhập") {
		t.Errorf("denied: %d", resp.StatusCode)
	}
	state = start(t, b, "/books")
	if resp, _ := b.do("GET", "/auth/google/callback?code=bad&state="+url.QueryEscape(state), "", nil); resp.StatusCode != 502 {
		t.Errorf("bad code: %d", resp.StatusCode)
	}

	state = start(t, b, "/books")
	resp, _ := b.do("GET", "/auth/google/callback?code=ok&state="+url.QueryEscape(state), "", nil)
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/books" {
		t.Fatalf("callback: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body := b.do("GET", "/me", "", nil)
	if !strings.Contains(body, "mc@gmail.com") || !strings.Contains(body, "<strong>Google</strong>") || !strings.Contains(body, "Đặt mật khẩu") {
		t.Errorf("me page:\n%s", body)
	}

	if resp, _ := b.form("/me/password", url.Values{"password": {"a brand new password"}}); resp.StatusCode != 200 {
		t.Errorf("set password: %d", resp.StatusCode)
	}
	if _, body := b.do("GET", "/me", "", nil); !strings.Contains(body, "Đổi mật khẩu") {
		t.Error("after setting a password the form asks for the current one")
	}
	if resp, _ := b.form("/me/password", url.Values{"current": {"wrong one!!"}, "password": {"another new password"}}); resp.StatusCode != 401 {
		t.Errorf("wrong current password: %d", resp.StatusCode)
	}

	// unverified Google email
	p.id = account.Identity{Provider: "google", Subject: "g2", Email: "x@gmail.com"}
	c := newBrowser(t, ts)
	state = start(t, c, "/")
	if resp, body := c.do("GET", "/auth/google/callback?code=ok&state="+url.QueryEscape(state), "", nil); resp.StatusCode != 401 || !strings.Contains(body, "chưa được xác minh") {
		t.Errorf("unverified: %d", resp.StatusCode)
	}
}

func TestNoGoogleButtonWhenDisabled(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	b := newBrowser(t, ts)
	if _, body := b.do("GET", "/login", "", nil); strings.Contains(body, "/auth/google") {
		t.Error("button shown without a provider")
	}
	if resp, _ := b.do("GET", "/auth/google", "", nil); resp.StatusCode != 404 {
		t.Errorf("disabled provider: %d", resp.StatusCode)
	}
}
