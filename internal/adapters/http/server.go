// Package httpadapter serves the JSON API (spec §6) and the HTMX interface.
package httpadapter

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"

	"playword/internal/adapters/http/views"
	"playword/internal/adapters/observability"
	"playword/internal/app"
	"playword/internal/domain/analytics"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/ports"
	"playword/pkg/vnphon"
)

//go:embed static
var staticFS embed.FS

// Options configure the server.
type Options struct {
	Log           *slog.Logger
	SearchTimeout time.Duration // spec §5: 2s
	RateLimitRPS  float64       // per client IP for search routes; 0 disables
	RateBurst     int
	TrustProxy    bool         // take the client IP from X-Forwarded-For
	Ready         func() error // nil error once the index is loaded
	Accounts      *app.Accounts
	SecureCookies bool    // set the Secure flag (off only for plain-HTTP development)
	LoginRPM      float64 // login/register attempts per minute per IP (spec: 5); 0 disables
	Slang         *app.Slang
	Ingest        *app.Ingester
	AdminToken    string                         // PLAYWORD_ADMIN_TOKEN: bearer token for the admin API
	SubmitRPM     float64                        // slang submissions per minute per IP (spec: 1); 0 disables
	Recorder      *app.Recorder                  // analytics events; nil disables
	Dashboard     *app.Dashboard                 // admin dashboard; nil hides it
	OAuth         map[string]ports.OAuthProvider // sign-in providers by name ("google"); empty hides the buttons
	PublicURL     string                         // https://playword.vn: canonical and Open Graph URLs, sitemap
	Contact       string                         // contact email on the legal pages and footer
	Version       string
}

// Server routes requests to the app service.
type Server struct {
	svc           *app.Service
	accounts      *app.Accounts
	slang         *app.Slang
	opts          Options
	mux           *http.ServeMux
	site          siteCache
	checkThrottle throttle
}

// New wires the routes; ctx bounds background work such as the rate limiter janitor.
func New(ctx context.Context, svc *app.Service, o Options) *Server {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.SearchTimeout <= 0 {
		o.SearchTimeout = 2 * time.Second
	}
	s := &Server{svc: svc, accounts: o.Accounts, slang: o.Slang, opts: o, mux: http.NewServeMux()}

	var limiter *ipLimiter
	if o.RateLimitRPS > 0 {
		limiter = newIPLimiter(ctx, o.RateLimitRPS, max(o.RateBurst, 1), o.TrustProxy)
	}
	search := func(h http.HandlerFunc) http.Handler { return limiter.wrap(withTimeout(o.SearchTimeout, h)) }
	plain := func(h http.HandlerFunc) http.Handler { return withTimeout(o.SearchTimeout, h) }

	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", cacheFor("public, max-age=3600", http.StripPrefix("/static/", http.FileServerFS(static))))

	s.mux.Handle("GET /api/v1/rhymes", search(s.apiRhymes))
	s.mux.Handle("GET /api/v1/words/{lang}/{text}", plain(s.apiWord))
	s.mux.Handle("GET /api/v1/parse", plain(s.apiParse))
	s.mux.Handle("GET /api/v1/suggest", plain(s.apiSuggest))

	s.mux.Handle("GET /{$}", search(s.pageHome))
	s.mux.Handle("GET /about", plain(s.pageAbout))
	s.mux.Handle("GET /sources", plain(s.pageSources))
	s.mux.Handle("GET /terms", plain(s.pageTerms))
	s.mux.Handle("GET /privacy", plain(s.pagePrivacy))
	s.mux.Handle("GET /robots.txt", plain(s.robots))
	s.mux.Handle("GET /sitemap.xml", plain(s.sitemap))
	s.mux.Handle("GET /", plain(s.pageNotFound))
	s.mux.Handle("GET /search", search(s.pageSearch))
	s.mux.Handle("GET /word/{lang}/{text}", plain(s.pageWord))
	s.mux.Handle("GET /dictionary", plain(s.pageDictionary))
	s.mux.Handle("GET /dictionary/suggest", plain(s.pageSuggest))
	s.mux.Handle("GET /view", plain(s.pageView))
	s.mux.Handle("GET /write", plain(s.pageWrite))
	s.mux.Handle("POST /write/check", plain(s.pageCheck))
	s.mux.Handle("POST /api/v1/rhymecheck", search(s.apiRhymeCheck))

	if s.accounts != nil {
		var loginLimiter *ipLimiter
		if o.LoginRPM > 0 {
			loginLimiter = newIPLimiter(ctx, o.LoginRPM/60, max(int(o.LoginRPM), 1), o.TrustProxy)
		}
		login := func(h http.HandlerFunc) http.Handler { return loginLimiter.wrap(withTimeout(o.SearchTimeout, h)) }
		s.mux.Handle("GET /login", plain(s.pageAuth(false)))
		s.mux.Handle("POST /login", login(s.pageAuth(false)))
		s.mux.Handle("GET /register", plain(s.pageAuth(true)))
		s.mux.Handle("POST /register", login(s.pageAuth(true)))
		s.mux.Handle("POST /logout", plain(s.pageLogout))
		s.mux.Handle("GET /me", plain(s.pageMe))
		s.mux.Handle("POST /me", plain(s.pageMe))
		s.mux.Handle("POST /me/delete", login(s.pageDeleteMe))
		s.mux.Handle("POST /me/password", login(s.pagePassword))
		s.mux.Handle("GET /auth/{provider}", plain(s.pageOAuthStart))
		// the token exchange is a round trip to the provider: allow more than a search
		s.mux.Handle("GET /auth/{provider}/callback", loginLimiter.wrap(withTimeout(15*time.Second, http.HandlerFunc(s.pageOAuthCallback))))
		s.mux.Handle("GET /books", plain(s.pageBooks))
		s.mux.Handle("POST /books", plain(s.pageBooks))
		s.mux.Handle("GET /books/{id}", plain(s.pageBook))
		s.mux.Handle("POST /books/{id}", plain(s.pageBook))
		s.mux.Handle("POST /books/{id}/delete", plain(s.pageDeleteBook))
		s.mux.Handle("POST /books/{id}/items", plain(s.pageAddItem))
		s.mux.Handle("POST /books/{id}/items/{item}/delete", plain(s.pageDeleteItem))
		s.mux.Handle("POST /books/{id}/items/{item}/note", plain(s.pageItemNote))
		s.mux.Handle("PUT /books/{id}/order", plain(s.pageOrder))
		s.mux.Handle("GET /books/{id}/export", plain(s.pageExport))
		s.mux.Handle("GET /books/{id}/write", plain(s.pageBookWrite))
		s.mux.Handle("GET /books/{id}/check", plain(s.pageBookCheck))
		s.mux.Handle("POST /books/{id}/lyrics", plain(s.pageLyrics))
		s.mux.Handle("POST /save", plain(s.pageSave))

		s.mux.Handle("GET /api/v1/auth/csrf", plain(s.apiCSRF))
		s.mux.Handle("POST /api/v1/auth/register", login(s.apiAuth(true)))
		s.mux.Handle("POST /api/v1/auth/login", login(s.apiAuth(false)))
		s.mux.Handle("POST /api/v1/auth/logout", plain(s.apiLogout))
		s.mux.Handle("GET /api/v1/me", plain(s.apiMe))
		s.mux.Handle("PATCH /api/v1/me", plain(s.apiMe))
		s.mux.Handle("DELETE /api/v1/me", login(s.apiMe))
		s.mux.Handle("GET /api/v1/books", plain(s.apiBooks))
		s.mux.Handle("POST /api/v1/books", plain(s.apiBooks))
		s.mux.Handle("GET /api/v1/books/{id}", plain(s.apiBook))
		s.mux.Handle("PATCH /api/v1/books/{id}", plain(s.apiBook))
		s.mux.Handle("DELETE /api/v1/books/{id}", plain(s.apiBook))
		s.mux.Handle("POST /api/v1/books/{id}/items", plain(s.apiAddItem))
		s.mux.Handle("PATCH /api/v1/books/{id}/items/{item}", plain(s.apiItem))
		s.mux.Handle("DELETE /api/v1/books/{id}/items/{item}", plain(s.apiItem))
		s.mux.Handle("PUT /api/v1/books/{id}/items/order", plain(s.pageOrder))
		s.mux.Handle("GET /api/v1/books/{id}/export", plain(s.pageExport))
		s.mux.Handle("GET /api/v1/public/books/{id}", plain(s.apiPublicBook))
	}

	if s.slang != nil {
		var submitLimiter *ipLimiter
		if o.SubmitRPM > 0 {
			submitLimiter = newIPLimiter(ctx, o.SubmitRPM/60, max(int(o.SubmitRPM), 1), o.TrustProxy)
		}
		submit := func(h http.HandlerFunc) http.Handler { return submitLimiter.wrap(withTimeout(o.SearchTimeout, h)) }
		s.mux.Handle("GET /slang/new", plain(s.pageSlangNew))
		s.mux.Handle("POST /slang/new", submit(s.pageSlangNew))
		s.mux.Handle("POST /api/v1/slang", submit(s.apiSubmitSlang))
		s.mux.Handle("GET /admin/slang", s.admin(s.pageAdminSlang))
		s.mux.Handle("POST /admin/slang/{id}/approve", s.admin(s.pageAdminReview(true)))
		s.mux.Handle("POST /admin/slang/{id}/reject", s.admin(s.pageAdminReview(false)))
		s.mux.Handle("GET /api/v1/admin/slang", s.admin(s.apiAdminSlang))
		s.mux.Handle("POST /api/v1/admin/slang/{id}/approve", s.admin(s.apiAdminReview(true)))
		s.mux.Handle("POST /api/v1/admin/slang/{id}/reject", s.admin(s.apiAdminReview(false)))
		s.mux.Handle("POST /api/v1/admin/entries/{id}/trend", s.admin(s.apiAdminTrend))
		s.mux.Handle("POST /api/v1/admin/ingest/{source}", s.admin(s.apiAdminIngest))
	}
	if o.Dashboard != nil {
		s.mux.Handle("GET /admin/dashboard", s.admin(s.pageDashboard))
		s.mux.Handle("GET /admin/users", s.admin(s.pageUsers))
		s.mux.Handle("GET /api/v1/admin/dashboard/summary", s.admin(s.apiDashSummary))
		s.mux.Handle("GET /api/v1/admin/dashboard/queries", s.admin(s.apiDashQueries))
		s.mux.Handle("GET /api/v1/admin/dashboard/breakdown", s.admin(s.apiDashBreakdown))
		s.mux.Handle("GET /api/v1/admin/dashboard/users", s.admin(s.apiDashUsers))
		s.mux.Handle("GET /api/v1/admin/dashboard/export.csv", s.admin(s.apiDashExport))
	}
	if s.slang != nil || o.Dashboard != nil {
		target := "/admin/slang"
		if o.Dashboard != nil {
			target = "/admin/dashboard"
		}
		s.mux.Handle("GET /admin", s.admin(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusSeeOther)
		}))
	}

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	s.mux.HandleFunc("GET /readyz", s.readyz)
	return s
}

// Handler is the root handler with logging, recovery and security headers.
func (s *Server) Handler() http.Handler {
	return logRecover(s.opts.Log, securityHeaders(s.identify(observability.RouteCapture(s.mux))))
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.opts.Ready != nil {
		if err := s.opts.Ready(); err != nil {
			writeProblemStatus(w, r, http.StatusServiceUnavailable, "not-ready", "Chưa sẵn sàng", err.Error())
			return
		}
	}
	_, _ = w.Write([]byte("ready"))
}

func cacheFor(v string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", v)
		h.ServeHTTP(w, r)
	})
}

// render writes a component. Pages carry the CSRF token and the user's name,
// so unless the handler marked the response public (a fragment without
// either), it must never be stored by a shared cache.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "HX-Request")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "render", "path", r.URL.Path, "err", err)
	}
}

// pageSearch serves / and /search. HTMX requests get only what they swap:
// the next page of hits for a cursor, the extra-dialect block, or the
// result section; everything else gets the full page (shareable URLs).
func (s *Server) pageSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	m := views.SearchModel{Page: s.page(r), Desk: r.URL.Query().Get("desk") == "1", Partial: isHTMX(r)}
	p, err := rhymeParamsFor(pageQuery(r.URL.Query()), m.Page.User)
	m.Params = p
	if !isHTMX(r) && m.Page.User != nil && s.accounts != nil {
		m.Page.Books, _ = s.accounts.Books(r.Context(), m.Page.User)
	}
	switch {
	case err != nil:
		m.Error = userMessage(err)
	case p.Text != "":
		m.Result, err = s.svc.SearchRhymes(r.Context(), p)
		if err != nil {
			m.Result, m.Error = nil, userMessage(err)
		}
		s.recordSearch(r, p, m.Result, start)
	}
	status := http.StatusOK
	if m.Error != "" {
		status, _, _ = classify(err)
	}
	if !isHTMX(r) {
		render(w, r, status, views.SearchPage(m))
		return
	}
	// htmx ignores 4xx/5xx swaps by default; errors are shown in-page.
	switch {
	case m.Result != nil && p.Cursor != "" && m.Desk:
		render(w, r, http.StatusOK, views.DeskHitItems(m.Result))
	case m.Result != nil && p.Cursor != "":
		render(w, r, http.StatusOK, views.HitItems(m.Result))
	case m.Result != nil && p.ExtraOver != "":
		render(w, r, http.StatusOK, views.Extra(m.Result, m.Desk))
	default:
		render(w, r, http.StatusOK, views.Results(m))
	}
}

func (s *Server) pageWord(w http.ResponseWriter, r *http.Request) {
	lang := entry.Lang(r.PathValue("lang"))
	e, err := s.svc.LookupWord(lang, r.PathValue("text"))
	if err != nil {
		if isHTMX(r) {
			render(w, r, http.StatusOK, templ.Raw(`<p class="msg err">`+templ.EscapeString(userMessage(err))+`</p>`))
			return
		}
		status, _, _ := classify(err)
		render(w, r, status, views.DictionaryPage(views.DictModel{Page: s.page(r), Query: r.PathValue("text"), Lang: lang, Error: userMessage(err)}))
		return
	}
	q := r.URL.Query()
	from := s.fromResults(r, lang)
	if from != nil {
		from.Entry = e
		s.record(r, analytics.Event{Type: analytics.ClickResult, Lang: string(lang), EntryID: e.ID,
			Query: queryText(from.Params.Text)})
	}
	if isHTMX(r) {
		w.Header().Set("Cache-Control", "private, max-age=300") // per-visitor: clicks are counted
		if from != nil {
			render(w, r, http.StatusOK, views.WordPanel(*from))
			return
		}
		render(w, r, http.StatusOK, views.Definition(e))
		return
	}
	if from == nil {
		s.record(r, analytics.Event{Type: analytics.ViewWord, Lang: string(lang), EntryID: e.ID})
	}
	saved := ""
	if q.Get("saved") != "" {
		saved = "saved"
		if currentUser(r) == nil {
			saved = "guest"
		}
	}
	render(w, r, http.StatusOK, views.WordPage(s.page(r), e, from, saved))
}

// fromResults is the search a word was opened from (?from=rhyme&q=…), which
// the save button records; nil when the word was opened directly.
func (s *Server) fromResults(r *http.Request, lang entry.Lang) *views.PanelModel {
	q := r.URL.Query()
	if q.Get("from") != "rhyme" {
		return nil
	}
	v := url.Values{"q": {q.Get("q")}, "lang": {string(lang)}, "dialect": {q.Get("dialect")}, "level": {q.Get("level")}}
	p, err := rhymeParamsFor(pageQuery(v), nil)
	if err != nil {
		p = app.DefaultRhymeParams(q.Get("q"))
		p.Lang = lang
	}
	m := &views.PanelModel{Params: p, CSRF: csrfToken(r), Desk: q.Get("desk") == "1"}
	if l, err := rhyme.ParseLevel(q.Get("hl")); err == nil {
		m.Level = views.LevelLabel(l)
	}
	next := *r.URL
	nq := next.Query()
	nq.Del("saved")
	nq.Set("saved", "1")
	next.RawQuery = nq.Encode()
	m.Next = next.RequestURI()
	return m
}

// pageQuery adapts query parameters from pages: a level from the other
// language (left over after switching language on the writing desk) falls
// back to the new language's default instead of failing.
func pageQuery(v url.Values) url.Values {
	l, err := rhyme.ParseLevel(v.Get("level"))
	if err == nil && l.IsEnglish() != (v.Get("lang") == string(entry.EN)) {
		v = maps.Clone(v)
		v.Del("level")
	}
	return v
}

// viewMode is how result lists show: the visitor's choice (cookie pw_view),
// else compact on phones and full on wide screens. Phones are told by the
// client hint Chromium sends, or "Mobi" in the user agent (others).
func viewMode(r *http.Request) string {
	if c, err := r.Cookie(viewCookie); err == nil && (c.Value == views.ViewCompact || c.Value == views.ViewFull) {
		return c.Value
	}
	if r.Header.Get("Sec-CH-UA-Mobile") == "?1" || strings.Contains(r.UserAgent(), "Mobi") {
		return views.ViewCompact
	}
	return views.ViewFull
}

// pageView switches the result view without JavaScript.
func (s *Server) pageView(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode != views.ViewCompact && mode != views.ViewFull {
		mode = views.ViewFull
	}
	c := s.cookie(viewCookie, mode, 365*24*time.Hour, false)
	http.SetCookie(w, c)
	http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}

func (s *Server) pageDictionary(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	lang := langParam(r)
	m := views.DictModel{Page: s.page(r), Query: q, Lang: lang}
	status := http.StatusOK
	if q != "" {
		e, err := s.svc.LookupWord(lang, q)
		switch {
		case err == nil:
			m.Entry = e
			s.record(r, analytics.Event{Type: analytics.ViewWord, Lang: string(lang), EntryID: e.ID})
		case errors.Is(err, app.ErrNotFound):
			status = http.StatusNotFound
			m.Suggestions, _ = s.svc.Suggest(lang, q, 10)
			m.Meanings, _ = s.svc.SearchMeanings(lang, q, 20)
		default:
			status, _, _ = classify(err)
			m.Error = userMessage(err)
		}
	}
	render(w, r, status, views.DictionaryPage(m))
}

func (s *Server) pageSuggest(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.Suggest(langParam(r), r.URL.Query().Get("q"), 8)
	if err != nil {
		render(w, r, http.StatusOK, templ.NopComponent)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	render(w, r, http.StatusOK, views.Suggestions(langParam(r), list))
}

// userMessage is the Vietnamese text shown in the page for an error.
func userMessage(err error) string {
	var se *vnphon.SyllableError
	if errors.As(err, &se) {
		return "“" + se.Input + "” không phải âm tiết tiếng Việt chuẩn — thử gõ đủ dấu."
	}
	_, _, title := classify(err)
	return title
}
