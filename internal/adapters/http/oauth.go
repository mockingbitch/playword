package httpadapter

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"playword/internal/domain/account"
	"playword/internal/domain/analytics"
)

// oauthCookie carries state, the PKCE verifier and the post-login target
// between /auth/{provider} and its callback. It is HttpOnly, scoped to
// /auth/ and lives 10 minutes; SameSite=Lax still sends it on the
// top-level redirect back from the provider.
const (
	oauthCookie = "pw_oauth"
	oauthTTL    = 10 * time.Minute
)

func (s *Server) oauthStateCookie(provider, value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{Name: oauthCookie, Value: value, Path: "/auth/" + provider, MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: s.opts.SecureCookies, SameSite: http.SameSiteLaxMode}
}

// pageOAuthStart sends the browser to the provider's consent page.
func (s *Server) pageOAuthStart(w http.ResponseWriter, r *http.Request) {
	p, ok := s.opts.OAuth[r.PathValue("provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, verifier := account.NewToken(), account.NewToken()+account.NewToken()
	next := safeNext(r.URL.Query().Get("next"))
	http.SetCookie(w, s.oauthStateCookie(p.Name(), state+"."+verifier+"."+next, oauthTTL))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.AuthURL(state, verifier), http.StatusFound)
}

// pageOAuthCallback finishes the sign-in: the state must match the cookie
// set by pageOAuthStart (login CSRF), then the code is exchanged with PKCE.
func (s *Server) pageOAuthCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := s.opts.OAuth[r.PathValue("provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	fail := func(status int, msg string) {
		render(w, r, status, s.authPageWithError(r, msg))
	}
	c, err := r.Cookie(oauthCookie)
	http.SetCookie(w, s.oauthStateCookie(p.Name(), "", -1)) // single use
	if err != nil {
		fail(http.StatusBadRequest, "Phiên đăng nhập đã hết hạn, hãy thử lại.")
		return
	}
	state, rest, _ := strings.Cut(c.Value, ".")
	verifier, next, _ := strings.Cut(rest, ".")
	q := r.URL.Query()
	if state == "" || verifier == "" || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
		fail(http.StatusBadRequest, "Phiên đăng nhập không hợp lệ, hãy thử lại.")
		return
	}
	if e := q.Get("error"); e != "" || q.Get("code") == "" {
		if e == "access_denied" {
			fail(http.StatusUnauthorized, "Bạn đã huỷ đăng nhập.")
			return
		}
		fail(http.StatusBadRequest, "Đăng nhập không thành công, hãy thử lại.")
		return
	}
	id, err := p.Exchange(r.Context(), q.Get("code"), verifier)
	if err != nil {
		s.opts.Log.Warn("oauth exchange", "provider", p.Name(), "err", err)
		fail(http.StatusBadGateway, "Không xác minh được tài khoản, hãy thử lại.")
		return
	}
	u, token, created, err := s.accounts.LoginOAuth(r.Context(), id, clientIP(r, s.opts.TrustProxy), r.UserAgent())
	if err != nil {
		if statusOf(err) == http.StatusInternalServerError && !errors.Is(err, account.ErrEmailNotVerified) {
			s.opts.Log.Error("oauth login", "provider", p.Name(), "err", err)
		}
		fail(statusOf(err), accountMessage(err))
		return
	}
	s.setSession(w, token)
	s.record(r.WithContext(context.WithValue(r.Context(), userKey, u)), analytics.Event{Type: analytics.Login})
	next = safeNext(next)
	if created {
		if items := guestItems(r); len(items) > 0 {
			if b, err := s.accounts.ImportGuest(r.Context(), u, items); err == nil && b != nil {
				s.setGuestItems(w, nil)
				next = "/books/" + b.ID
			}
		}
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// pagePassword sets or changes the password of the signed-in user.
func (s *Server) pagePassword(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	keep := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = c.Value
	}
	err := s.accounts.SetPassword(r.Context(), u, r.PostFormValue("current"), r.PostFormValue("password"), keep)
	m := s.meModel(r)
	if err != nil {
		if errors.Is(err, account.ErrBadCredentials) {
			m.Error = "Mật khẩu hiện tại không đúng."
		} else {
			m.Error = accountMessage(err)
		}
		renderMe(w, r, statusOf(err), m)
		return
	}
	m = s.meModel(r)
	m.Message = "Đã lưu mật khẩu. Các phiên đăng nhập khác đã bị đăng xuất."
	renderMe(w, r, http.StatusOK, m)
}
