package httpadapter

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"playword/internal/domain/account"
)

const (
	sessionCookie = "pw_session"
	csrfCookie    = "pw_csrf"
	guestCookie   = "pw_guest"
	csrfHeader    = "X-CSRF-Token"
	csrfField     = "csrf_token"
)

type ctxKey int

const (
	userKey ctxKey = iota
	csrfKey
)

// currentUser is the signed-in user of the request, or nil.
func currentUser(r *http.Request) *account.User {
	u, _ := r.Context().Value(userKey).(*account.User)
	return u
}

// csrfToken is the token pages embed for this client.
func csrfToken(r *http.Request) string {
	t, _ := r.Context().Value(csrfKey).(string)
	return t
}

func (s *Server) cookie(name, value string, maxAge time.Duration, httpOnly bool) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()), HttpOnly: httpOnly,
		Secure: s.opts.SecureCookies, SameSite: http.SameSiteLaxMode}
}

// identify attaches the session user and the CSRF token to every request,
// and rejects unsafe methods whose CSRF token does not match the cookie
// (double-submit; spec addendum §4).
func (s *Server) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if s.accounts != nil {
			if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
				if u, err := s.accounts.Authenticate(ctx, c.Value); err == nil {
					ctx = context.WithValue(ctx, userKey, u)
				} else if errors.Is(err, account.ErrUnauthenticated) {
					http.SetCookie(w, s.cookie(sessionCookie, "", -1, true))
				}
			}
		}
		if ctx.Value(userKey) == nil && s.opts.Recorder != nil {
			if c, err := r.Cookie(anonCookie); err != nil || c.Value == "" {
				anon := account.NewToken()[:22]
				http.SetCookie(w, s.cookie(anonCookie, anon, 365*24*time.Hour, true))
				r.AddCookie(&http.Cookie{Name: anonCookie, Value: anon})
			}
		}
		token := ""
		if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) >= 32 {
			token = c.Value
		} else {
			token = account.NewToken()
			http.SetCookie(w, s.cookie(csrfCookie, token, 365*24*time.Hour, true))
		}
		ctx = context.WithValue(ctx, csrfKey, token)
		bearer := s.bearerAdmin(r)
		if bearer {
			ctx = context.WithValue(ctx, adminTokenKey, true)
		}
		r = r.WithContext(ctx)

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if bearer {
				break // token auth, no ambient cookie credentials to forge
			}
			sent := r.Header.Get(csrfHeader)
			if sent == "" {
				sent = r.PostFormValue(csrfField)
			}
			if c, err := r.Cookie(csrfCookie); err != nil || subtle.ConstantTimeCompare([]byte(sent), []byte(c.Value)) != 1 {
				writeProblemStatus(w, r, http.StatusForbidden, "csrf", "Phiên làm việc không hợp lệ, hãy tải lại trang", "")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, s.cookie(sessionCookie, token, account.SessionTTL, true))
}

// guestItems reads the signed-out visitor's notebook from its cookie.
func guestItems(r *http.Request) []account.GuestItem {
	c, err := r.Cookie(guestCookie)
	if err != nil {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	var items []account.GuestItem
	if json.Unmarshal(b, &items) != nil || len(items) > account.GuestMaxItems {
		return nil
	}
	return items
}

func (s *Server) setGuestItems(w http.ResponseWriter, items []account.GuestItem) {
	if len(items) == 0 {
		http.SetCookie(w, s.cookie(guestCookie, "", -1, true))
		return
	}
	b, _ := json.Marshal(items)
	http.SetCookie(w, s.cookie(guestCookie, base64.RawURLEncoding.EncodeToString(b), 30*24*time.Hour, true))
}

// safeNext keeps post-login redirects on this site.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return "/"
	}
	return next
}
