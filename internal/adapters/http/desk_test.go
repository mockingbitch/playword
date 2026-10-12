package httpadapter

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWritingDesk(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	guest := newBrowser(t, ts)
	resp, body := guest.do("GET", "/write", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `data-desk="guest"`) || !strings.Contains(body, `id="lyrics"`) ||
		!strings.Contains(body, `name="desk" value="1"`) || strings.Contains(body, `id="lyrics-form"`) {
		t.Fatalf("guest desk: %d", resp.StatusCode)
	}

	b := newBrowser(t, ts)
	if r, _ := b.form("/register", url.Values{"email": {"w@example.vn"}, "password": {"correct horse battery"}}); r.StatusCode != 303 {
		t.Fatal("register")
	}
	if _, body := b.do("GET", "/write", "", nil); !strings.Contains(body, "Bắt đầu bài mới") || !strings.Contains(body, `name="next" value="write"`) {
		t.Error("a user without books is offered a new one")
	}
	r, _ := b.form("/books", url.Values{"title": {"Ngày tàn"}, "next": {"write"}})
	loc := r.Header.Get("Location")
	if r.StatusCode != 303 || !strings.HasSuffix(loc, "/write") {
		t.Fatalf("create from desk: %d %s", r.StatusCode, loc)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(loc, "/books/"), "/write")
	if r, _ := b.do("GET", "/write", "", nil); r.Header.Get("Location") != loc {
		t.Errorf("/write goes to the latest book: %s", r.Header.Get("Location"))
	}
	if _, body := b.do("GET", loc, "", nil); !strings.Contains(body, `data-desk="book"`) || !strings.Contains(body, `id="save-book" name="book_id" value="`+id+`"`) {
		t.Error("the desk saves rhymes into its own book")
	}

	// autosave (HTMX) and the plain "Lưu" button
	lyr := url.Values{"lyrics": {"Đêm nay phố vắng ngày tàn\r\nBước chân"}, "csrf_token": {b.csrf}}
	resp, body = b.do("POST", "/books/"+id+"/lyrics", "application/x-www-form-urlencoded", strings.NewReader(lyr.Encode()), "HX-Request", "true")
	if resp.StatusCode != 200 || !strings.Contains(body, "Đã lưu.") {
		t.Errorf("autosave: %d %s", resp.StatusCode, body)
	}
	if r, _ := b.form("/books/"+id+"/lyrics", url.Values{"lyrics": {"Đêm nay phố vắng ngày tàn\nBước chân lạc"}}); r.Header.Get("Location") != loc+"?saved=1" {
		t.Errorf("plain save: %s", r.Header.Get("Location"))
	}
	if _, body := b.do("GET", loc+"?saved=1", "", nil); !strings.Contains(body, "Bước chân lạc</textarea>") || !strings.Contains(body, "Đã lưu.") {
		t.Error("the desk shows the saved lyrics")
	}
	if _, body := b.do("GET", "/books/"+id, "", nil); !strings.Contains(body, `class="lyrics-view"`) || !strings.Contains(body, "Mở bàn viết") {
		t.Error("the book shows its lyrics")
	}
	if _, body := b.do("GET", "/books/"+id+"/export?format=md", "", nil); !strings.Contains(body, "## Lời") || !strings.Contains(body, "Bước chân lạc") {
		t.Errorf("export: %s", body)
	}
	if r, body := b.form("/books/"+id+"/lyrics", url.Values{"lyrics": {strings.Repeat("a", 20001)}}); r.StatusCode != 400 || !strings.Contains(body, "dài quá") {
		t.Errorf("too long: %d", r.StatusCode)
	}

	// someone else's book: no desk, no writing
	other := newBrowser(t, ts)
	other.form("/register", url.Values{"email": {"o@example.vn"}, "password": {"correct horse battery"}})
	if r, _ := other.do("GET", loc, "", nil); r.StatusCode != 404 {
		t.Errorf("other's desk: %d", r.StatusCode)
	}
	if r, _ := other.form("/books/"+id+"/lyrics", url.Values{"lyrics": {"x"}}); r.StatusCode != 404 {
		t.Errorf("other's lyrics: %d", r.StatusCode)
	}

	// the books API reads and writes lyrics
	if _, body := b.json("PATCH", "/api/v1/books/"+id, map[string]any{"lyrics": "verse 2"}); !strings.Contains(body, `"lyrics":"verse 2"`) {
		t.Errorf("patch: %s", body)
	}
	if _, body := b.do("GET", "/api/v1/books/"+id, "", nil); !strings.Contains(body, `"lyrics":"verse 2"`) {
		t.Errorf("get: %s", body)
	}
}

func TestDeskRhymes(t *testing.T) {
	ts, _ := testServer(t, 0)
	// a level left over from the other language falls back instead of failing
	resp, body := get(t, ts, "/search?q=money&lang=en&level=L25&desk=1", true)
	if resp.StatusCode != 200 || !strings.Contains(body, "honey") || !strings.Contains(body, `id="levels"`) || !strings.Contains(body, `value="family" checked`) {
		t.Errorf("desk language switch: %d %s", resp.StatusCode, body)
	}
	if _, body := get(t, ts, "/search?q="+q("ngày tàn")+"&desk=1", true); !strings.Contains(body, "desk=1") {
		t.Error("desk results open the panel with the insert button")
	}
	// the JSON API stays strict
	if resp, _ := get(t, ts, "/api/v1/rhymes?q=money&lang=en&level=L25", false); resp.StatusCode != 400 {
		t.Errorf("api: %d", resp.StatusCode)
	}
}

func TestViewMode(t *testing.T) {
	ts, _ := testServer(t, 0)
	req := func(ua, cookie string) string {
		r, _ := http.NewRequest("GET", ts.URL+"/?q="+q("ngày tàn"), nil)
		r.Header.Set("User-Agent", ua)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "pw_view", Value: cookie})
		}
		resp, err := ts.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	phone := "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Mobile/15E148"
	if b := req(phone, ""); !strings.Contains(b, `data-view="compact"`) {
		t.Error("phones default to compact")
	}
	if b := req("Mozilla/5.0 (X11; Linux x86_64)", ""); !strings.Contains(b, `data-view="full"`) {
		t.Error("desktops default to full")
	}
	if b := req(phone, "full"); !strings.Contains(b, `data-view="full"`) {
		t.Error("the cookie wins")
	}
	c := *ts.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(ts.URL + "/view?mode=compact&next=" + url.QueryEscape("/?q=tan"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var view *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "pw_view" {
			view = c
		}
	}
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/?q=tan" || view == nil || view.Value != "compact" || view.HttpOnly {
		t.Errorf("no-JS switch: %d %s %+v", resp.StatusCode, resp.Header.Get("Location"), view)
	}
}
