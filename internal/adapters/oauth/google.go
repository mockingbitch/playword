// Package oauth implements ports.OAuthProvider: Google sign-in with the
// authorization-code flow, PKCE and the OpenID Connect userinfo endpoint.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"

	"playword/internal/domain/account"
	"playword/internal/ports"
)

// GoogleUserinfo is Google's OpenID Connect userinfo endpoint.
const GoogleUserinfo = "https://openidconnect.googleapis.com/v1/userinfo"

// Google signs users in with their Google account.
type Google struct {
	cfg         oauth2.Config
	userinfoURL string
}

var _ ports.OAuthProvider = (*Google)(nil)

// NewGoogle configures the client registered in Google Cloud Console;
// redirectURL must be listed there exactly (…/auth/google/callback).
func NewGoogle(clientID, clientSecret, redirectURL string) *Google {
	return &Google{
		cfg: oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
			Endpoint: endpoints.Google, Scopes: []string{"openid", "email", "profile"}},
		userinfoURL: GoogleUserinfo,
	}
}

// WithEndpoints points the client at other URLs (tests).
func (g *Google) WithEndpoints(authURL, tokenURL, userinfoURL string) *Google {
	g.cfg.Endpoint = oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL}
	g.userinfoURL = userinfoURL
	return g
}

// Name is "google".
func (g *Google) Name() string { return "google" }

// AuthURL sends the browser to Google's consent page, with a PKCE challenge
// and the account chooser.
func (g *Google) AuthURL(state, verifier string) string {
	return g.cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"))
}

// Exchange trades the code for a token and reads the verified identity.
func (g *Google) Exchange(ctx context.Context, code, verifier string) (account.Identity, error) {
	tok, err := g.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return account.Identity{}, fmt.Errorf("google: token exchange: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userinfoURL, nil)
	if err != nil {
		return account.Identity{}, err
	}
	resp, err := g.cfg.Client(ctx, tok).Do(req)
	if err != nil {
		return account.Identity{}, fmt.Errorf("google: userinfo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return account.Identity{}, fmt.Errorf("google: userinfo: %s", resp.Status)
	}
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&info); err != nil {
		return account.Identity{}, fmt.Errorf("google: userinfo: %w", err)
	}
	if info.Sub == "" {
		return account.Identity{}, fmt.Errorf("google: userinfo without a subject")
	}
	return account.Identity{Provider: "google", Subject: info.Sub, Email: info.Email,
		EmailVerified: info.EmailVerified, Name: info.Name}, nil
}
