package httpadapter

import (
	"strings"
	"testing"

	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/app"
	"playword/internal/domain/entry"
)

func TestSitePages(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{PublicURL: "https://playword.example", Contact: "hello@playword.example"})
	cases := []struct {
		path   string
		status int
		ctype  string
		want   []string
		not    []string
	}{
		// the home page shows real results for its examples, never made-up words
		{"/", 200, "text/html", []string{"Gieo vần", `class="hero-demo"`, `m<b class="rh">ay</b>`, "Năm mức, một từ", "Chuẩn",
			`<link rel="canonical" href="https://playword.example/">`, `content="https://playword.example/static/og.png"`, `rel="manifest"`}, nil},
		{"/?q=" + q("ngày tàn"), 200, "text/html", []string{`id="rhyme-form"`, "Khớp 2 âm"}, []string{`class="hero"`}},
		{"/search?q=" + q("ngày tàn") + "&min_syl=2", 200, "text/html", []string{`<link rel="canonical" href="https://playword.example/search?q=`}, []string{`canonical" href="https://playword.example/search?min_syl`}},
		{"/about", 200, "text/html", []string{"Playword là gì?", `id="muc-van"`, `id="giong"`, "Perfect", "honey"}, nil},
		{"/sources", 200, "text/html", []string{"Nguồn dữ liệu", "Open English WordNet", "CC BY 4.0", "Be Vietnam Pro"}, nil},
		{"/terms", 200, "text/html", []string{"Điều khoản sử dụng", "mailto:hello@playword.example"}, nil},
		{"/privacy", 200, "text/html", []string{"Quyền riêng tư", "pw_session", "argon2id", "180 ngày", "không lưu địa chỉ IP"}, nil},
		{"/no-such-page", 404, "text/html", []string{"Trang này không có vần", `action="/search"`}, nil},
		{"/api/v1/nope", 404, "application/problem+json", []string{`"status":404`}, nil},
		{"/robots.txt", 200, "text/plain", []string{"Disallow: /api/", "Sitemap: https://playword.example/sitemap.xml"}, nil},
		{"/sitemap.xml", 200, "application/xml", []string{"<loc>https://playword.example/about</loc>"}, []string{"/admin"}},
		{"/static/fonts/be-vietnam-pro-latin-800.woff2", 200, "font/woff2", nil, nil},
		{"/static/manifest.webmanifest", 200, "application/manifest+json", []string{`"short_name": "Playword"`}, nil},
		{"/static/favicon.svg", 200, "image/svg+xml", nil, nil},
		{"/static/fonts/OFL.txt", 200, "text/plain", []string{"SIL Open Font License"}, nil},
	}
	for _, c := range cases {
		resp, body := get(t, ts, c.path, false)
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.path, resp.StatusCode, c.status)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, c.ctype) {
			t.Errorf("%s: content type %q, want %q", c.path, ct, c.ctype)
		}
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", c.path, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(body, n) {
				t.Errorf("%s: unexpected %q", c.path, n)
			}
		}
	}
	// a POST to an unknown path is not swallowed by the 404 page
	if resp, _ := get(t, ts, "/search", false); resp.StatusCode != 200 {
		t.Error("search still routes")
	}
}

func TestSiteWithoutPublicURL(t *testing.T) {
	ts, _ := testServer(t, 0)
	_, body := get(t, ts, "/", false)
	if strings.Contains(body, `rel="canonical"`) || strings.Contains(body, "og:image") || !strings.Contains(body, `og:title`) {
		t.Error("without a public URL there are no absolute links")
	}
	if resp, _ := get(t, ts, "/sitemap.xml", false); resp.StatusCode != 404 {
		t.Errorf("sitemap without a public URL: %d", resp.StatusCode)
	}
	if _, body := get(t, ts, "/terms", false); strings.Contains(body, "mailto:") {
		t.Error("no contact configured")
	}
}

func TestShowcase(t *testing.T) {
	ok := app.Hit{Entry: &rhymeindex.Entry{Senses: []entry.Sense{{Gloss: "Gặp dịp tốt."}}}}
	bad := app.Hit{Entry: &rhymeindex.Entry{Senses: []entry.Sense{{Gloss: "intensifier"}, {Gloss: "Slang for sexual intercourse"}}}}
	if !showcase(ok) || showcase(bad) {
		t.Error("showcase filter")
	}
}
