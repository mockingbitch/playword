package httpadapter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// browser is an HTTP client with cookies and the page's CSRF token.
type browser struct {
	t    *testing.T
	ts   *httptest.Server
	c    *http.Client
	csrf string
}

func newBrowser(t *testing.T, ts *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	c := *ts.Client() // a copy: ts.Client() is shared, and so would be its jar
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	b := &browser{t: t, ts: ts, c: &c}
	_, body := b.do("GET", "/api/v1/auth/csrf", "", nil)
	var tok struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal([]byte(body), &tok)
	b.csrf = tok.CSRFToken
	if b.csrf == "" {
		t.Fatal("no csrf token")
	}
	return b
}

func (b *browser) do(method, path, contentType string, body io.Reader, headers ...string) (*http.Response, string) {
	b.t.Helper()
	req, _ := http.NewRequest(method, b.ts.URL+path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, string(out)
}

func (b *browser) form(path string, v url.Values) (*http.Response, string) {
	v.Set("csrf_token", b.csrf)
	return b.do("POST", path, "application/x-www-form-urlencoded", strings.NewReader(v.Encode()))
}

func (b *browser) json(method, path string, body any) (*http.Response, string) {
	raw, _ := json.Marshal(body)
	return b.do(method, path, "application/json", strings.NewReader(string(raw)), "X-CSRF-Token", b.csrf)
}

func TestRegisterLoginLogoutForms(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	b := newBrowser(t, ts)

	resp, body := b.form("/register", url.Values{"email": {"mc@example.vn"}, "password": {"short"}})
	if resp.StatusCode != 400 || !strings.Contains(body, "ít nhất 10 ký tự") {
		t.Errorf("short password: %d", resp.StatusCode)
	}
	resp, _ = b.form("/register", url.Values{"email": {"mc@example.vn"}, "password": {"correct horse battery"}, "next": {"/books"}})
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/books" {
		t.Fatalf("register: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v", session)
	}
	if _, body := b.do("GET", "/", "", nil); !strings.Contains(body, `href="/me"`) {
		t.Error("signed-in nav")
	}
	if resp, _ := b.form("/logout", url.Values{}); resp.StatusCode != 303 {
		t.Errorf("logout: %d", resp.StatusCode)
	}
	if _, body := b.do("GET", "/", "", nil); !strings.Contains(body, `href="/login"`) {
		t.Error("signed out after logout")
	}
	if resp, _ := b.form("/login", url.Values{"email": {"mc@example.vn"}, "password": {"wrong password!"}}); resp.StatusCode != 401 {
		t.Errorf("bad login: %d", resp.StatusCode)
	}
	if resp, _ := b.form("/login", url.Values{"email": {"mc@example.vn"}, "password": {"correct horse battery"}, "next": {"//evil.example"}}); resp.Header.Get("Location") != "/" {
		t.Errorf("open redirect: %s", resp.Header.Get("Location"))
	}
	if resp, _ := b.do("GET", "/me", "", nil); resp.StatusCode != 200 {
		t.Errorf("me: %d", resp.StatusCode)
	}
}

func TestCSRF(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	b := newBrowser(t, ts)
	cases := []struct {
		name string
		do   func() *http.Response
	}{
		{"form without token", func() *http.Response {
			r, _ := b.do("POST", "/register", "application/x-www-form-urlencoded", strings.NewReader("email=a%40b.vn&password=correct+horse+battery"))
			return r
		}},
		{"wrong header", func() *http.Response {
			r, _ := b.do("POST", "/api/v1/auth/login", "application/json", strings.NewReader(`{}`), "X-CSRF-Token", "forged")
			return r
		}},
		{"other client's token", func() *http.Response {
			other := newBrowser(t, ts)
			r, _ := b.do("POST", "/api/v1/auth/login", "application/json", strings.NewReader(`{}`), "X-CSRF-Token", other.csrf)
			return r
		}},
	}
	for _, c := range cases {
		if r := c.do(); r.StatusCode != 403 {
			t.Errorf("%s: %d", c.name, r.StatusCode)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{LoginRPM: 5})
	b := newBrowser(t, ts)
	var codes []int
	for range 6 {
		r, _ := b.json("POST", "/api/v1/auth/login", map[string]string{"email": "x@example.vn", "password": "whatever123"})
		codes = append(codes, r.StatusCode)
	}
	if codes[4] != 401 || codes[5] != 429 {
		t.Errorf("codes = %v (5 per minute)", codes)
	}
}

func TestBooksAPI(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	owner, other := newBrowser(t, ts), newBrowser(t, ts)
	if r, _ := owner.json("POST", "/api/v1/auth/register", map[string]string{"email": "o@example.vn", "password": "correct horse battery"}); r.StatusCode != 201 {
		t.Fatalf("register %d", r.StatusCode)
	}
	if r, _ := other.json("POST", "/api/v1/auth/register", map[string]string{"email": "x@example.vn", "password": "correct horse battery"}); r.StatusCode != 201 {
		t.Fatal("register other")
	}
	r, body := owner.json("POST", "/api/v1/books", map[string]any{"title": "Verse 1"})
	var book struct{ ID string }
	_ = json.Unmarshal([]byte(body), &book)
	if r.StatusCode != 201 || book.ID == "" {
		t.Fatalf("create: %d %s", r.StatusCode, body)
	}
	base := "/api/v1/books/" + book.ID
	var ids []int64
	for _, item := range []map[string]any{{"entry_id": 1, "matched_from": "ngày tàn"}, {"custom_text": "chạy show"}} {
		r, body := owner.json("POST", base+"/items", item)
		var it struct{ ID int64 }
		_ = json.Unmarshal([]byte(body), &it)
		if r.StatusCode != 201 {
			t.Fatalf("add item: %d %s", r.StatusCode, body)
		}
		ids = append(ids, it.ID)
	}
	if r, _ := owner.json("PUT", base+"/items/order", map[string]any{"ids": []int64{ids[1], ids[0]}}); r.StatusCode != 204 {
		t.Errorf("order: %d", r.StatusCode)
	}
	_, body = owner.do("GET", base, "", nil)
	if strings.Index(body, "chạy show") > strings.Index(body, "entry-1") {
		t.Errorf("order not applied: %s", body)
	}

	// Private book: others and anonymous visitors get 404.
	for _, who := range []*browser{other, newBrowser(t, ts)} {
		if r, _ := who.do("GET", base, "", nil); r.StatusCode != 404 {
			t.Errorf("private book: %d", r.StatusCode)
		}
	}
	if r, _ := other.json("POST", base+"/items", map[string]any{"custom_text": "x"}); r.StatusCode != 404 {
		t.Errorf("other adds: %d", r.StatusCode)
	}
	if r, _ := other.json("DELETE", base, nil); r.StatusCode != 404 {
		t.Errorf("other deletes: %d", r.StatusCode)
	}

	if r, _ := owner.json("PATCH", base, map[string]any{"is_public": true}); r.StatusCode != 200 {
		t.Errorf("publish: %d", r.StatusCode)
	}
	anon := newBrowser(t, ts)
	if r, body := anon.do("GET", "/api/v1/public/books/"+book.ID, "", nil); r.StatusCode != 200 || !strings.Contains(body, "chạy show") {
		t.Errorf("public read: %d %s", r.StatusCode, body)
	}
	if r, body := anon.do("GET", "/books/"+book.ID, "", nil); r.StatusCode != 200 || strings.Contains(body, `class="grip"`) {
		t.Errorf("public page is read-only: %d", r.StatusCode)
	}
	if r, body := anon.do("GET", base+"/export?format=md", "", nil); r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Disposition"), "Verse%201.md") || !strings.HasPrefix(body, "# Verse 1") {
		t.Errorf("export: %d %s", r.StatusCode, r.Header.Get("Content-Disposition"))
	}

	if r, _ := owner.json("PATCH", base+"/items/"+itoa64(ids[1]), map[string]string{"note": "hook"}); r.StatusCode != 204 {
		t.Errorf("note: %d", r.StatusCode)
	}
	if r, _ := owner.json("DELETE", base+"/items/"+itoa64(ids[0]), nil); r.StatusCode != 204 {
		t.Errorf("delete item: %d", r.StatusCode)
	}
	if r, body := owner.json("GET", "/api/v1/me", nil); r.StatusCode != 200 || !strings.Contains(body, `"email":"o@example.vn"`) {
		t.Errorf("me: %s", body)
	}
	if r, body := owner.json("PATCH", "/api/v1/me", map[string]string{"default_lang": "en", "default_level": "perfect"}); r.StatusCode != 200 || !strings.Contains(body, `"default_level":"perfect"`) {
		t.Errorf("settings: %d %s", r.StatusCode, body)
	}
	// Saved defaults shape searches that do not say otherwise.
	if _, body := owner.do("GET", "/api/v1/rhymes?q=money", "", nil); !strings.Contains(body, `"level":"perfect"`) || !strings.Contains(body, `"lang":"en"`) {
		t.Errorf("defaults: %s", body)
	}
	if r, _ := owner.json("DELETE", base, nil); r.StatusCode != 204 {
		t.Errorf("delete book: %d", r.StatusCode)
	}
	if r, _ := owner.json("DELETE", "/api/v1/me", map[string]string{"password": "correct horse battery"}); r.StatusCode != 204 {
		t.Errorf("delete account: %d", r.StatusCode)
	}
	if r, _ := owner.json("GET", "/api/v1/me", nil); r.StatusCode != 401 {
		t.Errorf("after delete: %d", r.StatusCode)
	}
}

func TestSaveButton(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	guest := newBrowser(t, ts)
	save := url.Values{"entry_id": {"1"}, "lang": {"vi"}, "dialect": {"bac"}, "level": {"L25"}, "matched_from": {"ngày tàn"}}
	hx := func(b *browser, v url.Values) (*http.Response, string) {
		v.Set("csrf_token", b.csrf)
		return b.do("POST", "/save", "application/x-www-form-urlencoded", strings.NewReader(v.Encode()), "HX-Request", "true")
	}
	r, body := hx(guest, cloneV(save))
	if r.StatusCode != 200 || !strings.Contains(body, "Đã lưu tạm") {
		t.Fatalf("guest save: %s", body)
	}
	if _, body := guest.do("GET", "/books", "", nil); !strings.Contains(body, "may mắn") || !strings.Contains(body, "1/20") {
		t.Errorf("guest notebook: %s", body)
	}
	// Registering moves the guest notebook into the account.
	r, _ = guest.form("/register", url.Values{"email": {"g@example.vn"}, "password": {"correct horse battery"}})
	if loc := r.Header.Get("Location"); !strings.HasPrefix(loc, "/books/") {
		t.Fatalf("import redirect: %s", loc)
	}
	if _, body := guest.do("GET", r.Header.Get("Location"), "", nil); !strings.Contains(body, "Sổ tạm") || !strings.Contains(body, "vần với “ngày tàn”") {
		t.Errorf("imported book: %s", body)
	}
	if _, body := hx(guest, cloneV(save)); !strings.Contains(body, "Đã lưu vào sổ") {
		t.Errorf("user save: %s", body)
	}
	// Without JavaScript the save form posts and comes back.
	plain := cloneV(save)
	plain.Set("next", "/word/vi/may%20m%E1%BA%AFn?from=rhyme&saved=1")
	if r, _ := guest.form("/save", plain); r.StatusCode != 303 || r.Header.Get("Location") != plain.Get("next") {
		t.Errorf("plain save: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	if r, _ := guest.form("/save", url.Values{"entry_id": {"1"}, "next": {"//evil.example"}}); r.Header.Get("Location") != "/" {
		t.Errorf("open redirect: %s", r.Header.Get("Location"))
	}
	if _, body := guest.do("GET", "/word/vi/may%20m%E1%BA%AFn?from=rhyme&saved=1", "", nil); !strings.Contains(body, "Đã lưu vào sổ") {
		t.Error("the word page shows the saved state")
	}
	if _, body := guest.do("GET", "/?q=ng%C3%A0y+t%C3%A0n", "", nil); !strings.Contains(body, `id="save-book"`) || !strings.Contains(body, `hx-target="#panel"`) {
		t.Error("search page opens words in the panel, which saves into a book")
	}
}

func cloneV(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
