package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeGoogle checks the PKCE pair the way Google does.
func fakeGoogle(t *testing.T) *httptest.Server {
	var challenge string
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		challenge = r.URL.Query().Get("code_challenge")
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": "1234", "email": "mc@gmail.com", "email_verified": true, "name": "MC"})
	})
	return httptest.NewServer(mux)
}

func TestGoogleFlow(t *testing.T) {
	srv := fakeGoogle(t)
	defer srv.Close()
	g := NewGoogle("client-id", "secret", "http://localhost:8020/auth/google/callback").
		WithEndpoints(srv.URL+"/auth", srv.URL+"/token", srv.URL+"/userinfo")
	verifier := "verifier-0123456789-0123456789-0123456789-0123456789"
	u, err := url.Parse(g.AuthURL("state-1", verifier))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "state-1" || q.Get("code_challenge_method") != "S256" || q.Get("prompt") != "select_account" ||
		!strings.Contains(q.Get("scope"), "email") || q.Get("redirect_uri") != "http://localhost:8020/auth/google/callback" {
		t.Errorf("auth url = %s", u)
	}
	resp, _ := http.Get(u.String()) // the fake records the challenge
	_ = resp.Body.Close()

	id, err := g.Exchange(context.Background(), "good-code", verifier)
	if err != nil || id.Subject != "1234" || id.Email != "mc@gmail.com" || !id.EmailVerified || id.Provider != "google" || g.Name() != "google" {
		t.Fatalf("%+v %v", id, err)
	}
	if _, err := g.Exchange(context.Background(), "good-code", "wrong-verifier"); err == nil {
		t.Error("a wrong PKCE verifier must fail")
	}
	if _, err := g.Exchange(context.Background(), "bad-code", verifier); err == nil {
		t.Error("a bad code must fail")
	}
}
