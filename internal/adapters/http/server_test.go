package httpadapter

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/app"
	"playword/internal/app/apptest"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ingest"
	"playword/internal/ports"
	"playword/pkg/vnphon"
)

var update = flag.Bool("update", false, "rewrite golden files")

type fixedIndex struct{ s *rhymeindex.Snapshot }

func (f fixedIndex) Load() *rhymeindex.Snapshot { return f.s }

// testServer serves a small in-memory dictionary; rps > 0 enables the rate limit.
func testServer(t *testing.T, rps float64) (*httptest.Server, *app.Service) {
	ts, svc, _ := testServerWith(t, Options{RateLimitRPS: rps, RateBurst: 2})
	return ts, svc
}

// testServerWith also wires accounts on an in-memory store.
func testServerWith(t *testing.T, o Options) (*httptest.Server, *app.Service, *apptest.Accounts) {
	t.Helper()
	e, err := rhyme.LoadDir("../../../configs")
	if err != nil {
		t.Fatal(err)
	}
	words := []struct {
		text, gloss string
		zipf        float64
		tags        []string
	}{
		{"may mắn", "Gặp dịp tốt.", 4.4, nil},
		{"ngày tàn", "Ngày suy tàn.", 2, nil},
		{"cay đắng", "Xót xa trong lòng.", 3.4, nil},
		{"tay trắng", "Không có vốn liếng.", 2.9, nil},
		{"Thái An", "Tên xã.", 3, []string{"proper_noun"}},
		{"tàn", "Lụi, sắp hết.", 4.9, nil},
		{"tàng", "Cũ, sắp hỏng.", 2, nil},
		{"lan", "Loài hoa.", 4, nil},
		{"wifi", "Mạng không dây.", 3, nil},
	}
	data := &ports.IndexData{ConfigID: e.ConfigID(), Syllables: map[int64][]vnphon.Syllable{}, Senses: map[int64][]entry.Sense{}}
	for i, w := range words {
		id := int64(i + 1)
		data.Entries = append(data.Entries, ports.IndexEntry{ID: id, Headword: w.text, Normalized: entry.Normalize(w.text),
			Source: "test", Zipf: w.zipf, Tags: w.tags})
		data.Senses[id] = []entry.Sense{{Gloss: w.gloss, Examples: []string{"ví dụ"}}}
		syl, err := vnphon.ParsePhrase(w.text)
		if err != nil {
			continue // not rhymable, still in the dictionary
		}
		data.Syllables[id] = syl
	}
	data.Segments = map[int64][]en.Seg{}
	data.Pronunciations = map[string][]string{}
	for i, w := range []struct{ text, pron, gloss string }{
		{"money", "M AH1 N IY0", "medium of exchange"},
		{"honey", "HH AH1 N IY0", "sweet liquid made by bees"},
		{"tummy", "T AH1 M IY0", "belly"},
		{"mono", "M OW1 N OW0", "monophonic sound"},
	} {
		id := int64(100 + i)
		phones := en.ParsePhones(w.pron)
		segs, _ := en.Segments(phones)
		data.Pronunciations[w.text] = phones
		data.Entries = append(data.Entries, ports.IndexEntry{ID: id, Lang: entry.EN, Headword: w.text, Normalized: w.text, Source: "test", Confidence: 1})
		data.Senses[id] = []entry.Sense{{POS: "noun", Gloss: w.gloss}}
		data.Segments[id] = segs
	}
	snap, err := rhymeindex.Build(data, e)
	if err != nil {
		t.Fatal(err)
	}
	snap.Gen = 1
	svc := app.NewService(fixedIndex{snap}, e, 100)
	store := apptest.NewAccounts()
	o.Accounts = app.NewAccounts(store, "salt")
	ing := apptest.NewIngest()
	o.Slang = app.NewSlang(apptest.NewSlang(ing), ingest.Pipeline{Store: ing, Engine: e}, nil)
	srv := New(context.Background(), svc, o)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, svc, store
}

func get(t *testing.T, ts *httptest.Server, path string, htmx bool) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func q(s string) string { return url.QueryEscape(s) }

func TestAPIRhymes(t *testing.T) {
	ts, _ := testServer(t, 0)
	resp, body := get(t, ts, "/api/v1/rhymes?q="+q("ngày tàn")+"&limit=2", false)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var r struct {
		Query struct {
			Syllables []map[string]any
		}
		Results []struct {
			Text        string
			Matched     int `json:"matched_syllables"`
			Level       string
			Phonetic    []string
			Definitions []struct{ Gloss string }
			Alignment   struct{ Start, End int }
			IsProper    bool `json:"is_proper"`
		}
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Query.Syllables) != 2 || len(r.Results) != 2 || r.NextCursor == "" {
		t.Fatalf("%+v", r)
	}
	first := r.Results[0]
	if first.Text != "may mắn" || first.Matched != 2 || first.Level != "L25" ||
		strings.Join(first.Phonetic, " ") != "m-AW-j-1 m-AW-n-3" || first.Definitions[0].Gloss != "Gặp dịp tốt." ||
		first.Alignment.End != 2 {
		t.Errorf("first = %+v", first)
	}

	// Following the cursor continues the list; the proper noun comes last.
	_, body = get(t, ts, "/api/v1/rhymes?q="+q("ngày tàn")+"&limit=2&cursor="+r.NextCursor, false)
	var page2 struct {
		Results []struct {
			Text     string
			IsProper bool `json:"is_proper"`
		}
	}
	_ = json.Unmarshal([]byte(body), &page2)
	if len(page2.Results) != 2 || page2.Results[1].Text != "Thái An" || !page2.Results[1].IsProper {
		t.Errorf("page 2 = %+v", page2)
	}
}

func TestAPIProblems(t *testing.T) {
	ts, _ := testServer(t, 0)
	cases := map[string]int{
		"/api/v1/rhymes":                      400,
		"/api/v1/rhymes?q=wifi":               422,
		"/api/v1/rhymes?q=tan&level=L9":       400,
		"/api/v1/rhymes?q=tan&min_syl=x":      400,
		"/api/v1/rhymes?q=tan&slang=maybe":    400,
		"/api/v1/rhymes?q=tan&dialect=hue":    400,
		"/api/v1/rhymes?q=tan&match=middle":   400,
		"/api/v1/rhymes?q=tan&extra_over=hue": 400,
		"/api/v1/rhymes?q=tan&lang=en":        422,
		"/api/v1/rhymes?q=tan&cursor=***":     400,
		"/api/v1/rhymes?q=tan&cursor=bm9wZQ":  400,
		"/api/v1/words/vi/" + q("không có"):   404,
		"/api/v1/words/en/hello":              404,
		"/api/v1/parse?q=wifi":                422,
		"/api/v1/parse?q=tan&lang=en":         422,
	}
	for path, want := range cases {
		resp, body := get(t, ts, path, false)
		if resp.StatusCode != want || resp.Header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
			continue
		}
		var p problem
		if err := json.Unmarshal([]byte(body), &p); err != nil || p.Status != want || p.Type == "" || p.Instance == "" {
			t.Errorf("%s: problem body %s", path, body)
		}
	}
}

func TestAPIWordParseSuggest(t *testing.T) {
	ts, _ := testServer(t, 0)
	_, body := get(t, ts, "/api/v1/words/vi/"+url.PathEscape("WIFI"), false)
	if !strings.Contains(body, `"rhymable":false`) || !strings.Contains(body, `"text":"wifi"`) {
		t.Errorf("word: %s", body)
	}
	_, body = get(t, ts, "/api/v1/parse?q="+q("ngày tàn"), false)
	if !strings.Contains(body, `"nam":{"L0":"AW.j.2 A.ng.2"`) {
		t.Errorf("parse: %s", body)
	}
	_, body = get(t, ts, "/api/v1/suggest?q=may", false)
	if !strings.Contains(body, `"text":"may mắn"`) {
		t.Errorf("suggest: %s", body)
	}
}

func TestPages(t *testing.T) {
	ts, _ := testServer(t, 0)
	cases := []struct {
		path   string
		htmx   bool
		status int
		want   []string
		not    []string
	}{
		{"/", false, 200, []string{`class="hero"`, `action="/search"`, "Gieo vần", "Năm mức"}, []string{`id="rhyme-form"`}},
		{"/search", false, 200, []string{`<form id="rhyme-form"`, `hx-get="/search"`, "Thử:"}, nil},
		{"/?q=" + q("ngày tàn"), false, 200, []string{"<!doctype html>", `m<b class="rh">ay</b>`, "Khớp 2 âm"}, nil},
		{"/search?q=" + q("ngày tàn"), true, 200, []string{`<section id="results"`, "+ Nam"}, []string{"<!doctype html>"}},
		{"/search?q=wifi", true, 200, []string{"không phải âm tiết tiếng Việt chuẩn"}, nil},
		{"/search?q=wifi", false, 422, []string{"không phải âm tiết"}, nil},
		{"/search?q=" + q("tàn") + "&dialect=nam&extra_over=bac&level=L0", true, 200, []string{"Khớp thêm khi đọc giọng Nam", `t<b class="rh">àng</b>`}, []string{`l<b class="rh">an</b>`}},
		{"/word/vi/" + url.PathEscape("may mắn"), true, 200, []string{`class="defbody"`, "Gặp dịp tốt."}, []string{"<!doctype html>"}},
		{"/word/vi/" + url.PathEscape("may mắn"), false, 200, []string{"<!doctype html>", `<h1 class="hw">may mắn</h1>`, "Vần", "<b>ay</b>"}, []string{"Lưu vào sổ"}},
		{"/word/vi/" + url.PathEscape("may mắn") + "?from=rhyme&q=" + q("ngày tàn") + "&hl=L25", false, 200, []string{"Mức khớp: Rap-flex", `action="/save"`, "Vần cho “ngày tàn”"}, nil},
		{"/word/vi/" + url.PathEscape("may mắn") + "?from=rhyme&q=" + q("ngày tàn"), true, 200, []string{`class="panel-body"`, `hx-post="/save"`, "Gặp dịp tốt."}, []string{"<!doctype html>", "Chèn vào bài"}},
		{"/word/vi/" + url.PathEscape("may mắn") + "?from=rhyme&desk=1&q=" + q("ngày tàn"), true, 200, []string{`data-insert="may mắn"`}, nil},
		{"/word/vi/nope", true, 200, []string{`class="msg err"`}, nil},
		{"/word/vi/nope", false, 404, []string{"Không tìm thấy"}, nil},
		{"/dictionary", false, 200, []string{`id="dq"`}, nil},
		{"/dictionary?q=" + q("may mắn"), false, 200, []string{`<h1 class="hw">may mắn</h1>`, "Tìm vần cho"}, nil},
		{"/dictionary?q=wifi", false, 200, []string{`<h1 class="hw">wifi</h1>`}, []string{"Tìm vần cho"}},
		{"/dictionary?q=mayman", false, 404, []string{"Có phải bạn muốn tìm", "may mắn"}, nil},
		{"/dictionary/suggest?q=ngayf", true, 200, []string{"ngày tàn"}, nil},
		{"/static/app.css", false, 200, []string{"--accent"}, nil},
		{"/healthz", false, 200, []string{"ok"}, nil},
		{"/readyz", false, 200, []string{"ready"}, nil},
	}
	for _, c := range cases {
		resp, body := get(t, ts, c.path, c.htmx)
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.path, resp.StatusCode, c.status)
		}
		for _, w := range c.want {
			if !strings.Contains(strings.ToLower(body), strings.ToLower(w)) {
				t.Errorf("%s: missing %q", c.path, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(strings.ToLower(body), strings.ToLower(n)) {
				t.Errorf("%s: unexpected %q", c.path, n)
			}
		}
	}
	resp, _ := get(t, ts, "/", false)
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
}

func TestInfiniteScroll(t *testing.T) {
	ts, _ := testServer(t, 0)
	_, body := get(t, ts, "/search?q="+q("ngày tàn")+"&limit=2", true)
	i := strings.Index(body, `class="more" hx-get="`)
	if i < 0 {
		t.Fatalf("no sentinel in %s", body)
	}
	next := body[i+len(`class="more" hx-get="`):]
	end := strings.Index(next, `"`)
	if end < 0 {
		t.Fatal("unterminated hx-get")
	}
	next = strings.ReplaceAll(next[:end], "&amp;", "&")
	_, page2 := get(t, ts, next, true)
	if strings.Contains(page2, "<section") || !strings.Contains(page2, "Tên xã.") || strings.Contains(page2, `class="more"`) {
		t.Errorf("page 2 must be bare items without a further sentinel: %s", page2)
	}
}

func TestRateLimitAndReady(t *testing.T) {
	ts, _ := testServer(t, 0.001)
	codes := []int{}
	for range 3 {
		resp, _ := get(t, ts, "/api/v1/rhymes?q=tan", false)
		codes = append(codes, resp.StatusCode)
		if resp.StatusCode == 429 && resp.Header.Get("Retry-After") == "" {
			t.Error("429 needs Retry-After")
		}
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
		t.Errorf("codes = %v (burst 2)", codes)
	}

	notReady := New(context.Background(), app.NewService(fixedIndex{}, nil, 1), Options{Ready: func() error { return app.ErrNotReady }})
	empty := httptest.NewServer(notReady.Handler())
	defer empty.Close()
	for _, path := range []string{"/readyz", "/api/v1/rhymes?q=tan"} {
		if resp, _ := get(t, empty, path, false); resp.StatusCode != 503 {
			t.Errorf("%s: %d, want 503", path, resp.StatusCode)
		}
	}
}

// Golden test for the HTMX result partial (spec §7): run with -update to
// accept an intended change.
func TestGoldenResultsPartial(t *testing.T) {
	ts, _ := testServer(t, 0)
	_, body := get(t, ts, "/search?q="+q("ngày tàn")+"&limit=3", true)
	path := "testdata/results_ngay_tan.golden.html"
	if *update {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update once)", err)
	}
	if body != string(want) {
		t.Errorf("partial changed; diff against %s or rerun with -update\n%s", path, body)
	}
}

func TestEnglish(t *testing.T) {
	ts, _ := testServer(t, 0)
	_, body := get(t, ts, "/api/v1/rhymes?q=money&lang=en&limit=5", false)
	var r struct {
		Query struct {
			Level    string
			Phonetic []string
		}
		Results []struct {
			Text     string
			Level    string
			Phonetic []string
		}
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	// Default English level is family: honey (perfect), tummy (family); never mono or money itself.
	if r.Query.Level != "family" || strings.Join(r.Query.Phonetic, "|") != "AH1 N|IY0" || len(r.Results) != 2 ||
		r.Results[0].Text != "honey" || r.Results[0].Level != "perfect" || r.Results[1].Text != "tummy" {
		t.Errorf("%s", body)
	}
	_, body = get(t, ts, "/api/v1/words/en/honey", false)
	if !strings.Contains(body, `"phonetic":["AH1 N","IY0"]`) {
		t.Errorf("word: %s", body)
	}
	_, body = get(t, ts, "/api/v1/parse?q=moneys&lang=en", false)
	if !strings.Contains(body, `"phonetic_confidence":0.7`) {
		t.Errorf("derived pronunciation: %s", body)
	}
	_, body = get(t, ts, "/?q=money&lang=en", false)
	for _, want := range []string{`name="lang" value="en"`, "Perfect", `<b class="rh">honey</b>`} {
		if !strings.Contains(body, want) {
			t.Errorf("page: missing %q", want)
		}
	}
	if strings.Contains(body, `type="radio" name="dialect"`) {
		t.Error("English has no dialect chips")
	}
	_, body = get(t, ts, "/dictionary?q=honey&lang=en", false)
	if !strings.Contains(body, `<h1 class="hw">honey</h1>`) || !strings.Contains(body, "lang=en") {
		t.Errorf("dictionary: %s", body)
	}
	_, body = get(t, ts, "/dictionary/suggest?q=hon&lang=en", true)
	if !strings.Contains(body, "honey") || !strings.Contains(body, "lang=en") {
		t.Errorf("suggest: %s", body)
	}
}

func TestEnglishHighlight(t *testing.T) {
	ts, _ := testServer(t, 0)
	_, body := get(t, ts, "/search?q=money&lang=en", true)
	if !strings.Contains(body, `<b class="rh">honey</b>`) || !strings.Contains(body, `title="AH1 N IY0"`) {
		t.Errorf("%s", body)
	}
}
