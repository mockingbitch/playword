package httpadapter

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

const verse = "Đêm nay phố vắng ngày tàn\nĐứng nhìn hải quân đi qua\nChạy án cả một đời ta\nMỗi hai tuần lại xa một người"

func TestRhymeCheckAPI(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	b := newBrowser(t, ts)
	resp, body := b.json("POST", "/api/v1/rhymecheck", map[string]any{"text": verse, "level": "L25", "dialect": "bac"})
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var out checkJSON
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Groups) != 2 || out.Groups[0].Letter != "A" || out.Groups[0].Label != "ay-an" || out.Groups[1].Label != "ai-uân" ||
		out.Groups[0].Slot == out.Groups[1].Slot || out.Groups[0].Slot == 0 {
		t.Errorf("groups: %+v", out.Groups)
	}
	u := []rune(verse) // all BMP here: UTF-16 offsets are rune offsets
	var words []string
	for _, tk := range out.Tokens {
		words = append(words, string(u[tk.Start:tk.End])+"="+string(rune('A'+tk.Group-1)))
	}
	if got := strings.Join(words, " "); got != "ngày=A tàn=A hải=B quân=B Chạy=A án=A hai=B tuần=B" {
		t.Errorf("tokens: %s", got)
	}
	if len(out.Lines) != 4 || out.Lines[0] != "A" || out.Lines[2] != "" || len(out.Schemes) != 1 {
		t.Errorf("lines %v schemes %v", out.Lines, out.Schemes)
	}
	// bad input
	if resp, _ := b.json("POST", "/api/v1/rhymecheck", map[string]any{"text": strings.Repeat("a", 20001)}); resp.StatusCode != 400 {
		t.Errorf("too long: %d", resp.StatusCode)
	}
	if resp, _ := b.json("POST", "/api/v1/rhymecheck", map[string]any{"text": verse, "level": "family"}); resp.StatusCode != 200 {
		t.Errorf("an English level falls back to the Vietnamese default: %d", resp.StatusCode)
	}
	if resp, _ := b.do("POST", "/api/v1/rhymecheck", "application/json", strings.NewReader(`{"text":"x"}`)); resp.StatusCode != 403 {
		t.Errorf("CSRF: %d", resp.StatusCode)
	}
}

func TestRhymeCheckPages(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{})
	b := newBrowser(t, ts)
	// without JavaScript the desk's button posts to the coloured view
	if _, body := b.do("GET", "/write", "", nil); !strings.Contains(body, `formaction="/write/check"`) || !strings.Contains(body, `id="lyrics-mirror"`) {
		t.Error("desk has the rhyme check")
	}
	resp, body := b.form("/write/check", url.Values{"lyrics": {verse}, "next": {"/write"}})
	if resp.StatusCode != 200 || !strings.Contains(body, `<mark class="rg rg1 ru0" data-k="A">ngày tàn</mark>`) || !strings.Contains(body, `data-k="B">hai tuần</mark>`) {
		t.Errorf("check page: %d", resp.StatusCode)
	}
	for _, want := range []string{"2 nhóm vần", "Sơ đồ:", `class="cl-scheme"`, "-ay-an"} {
		if !strings.Contains(body, want) {
			t.Errorf("check page lacks %q", want)
		}
	}
	if resp, _ := b.form("/write/check", url.Values{"lyrics": {strings.Repeat("a", 20001)}}); resp.StatusCode != 400 {
		t.Errorf("too long: %d", resp.StatusCode)
	}

	// a book's lyrics, coloured, for whoever may read the book
	b.form("/register", url.Values{"email": {"rc@example.vn"}, "password": {"correct horse battery"}})
	r, _ := b.form("/books", url.Values{"title": {"Bài"}, "next": {"write"}})
	id := strings.TrimSuffix(strings.TrimPrefix(r.Header.Get("Location"), "/books/"), "/write")
	b.form("/books/"+id+"/lyrics", url.Values{"lyrics": {verse}})
	if _, body := b.do("GET", "/books/"+id, "", nil); !strings.Contains(body, `href="/books/`+id+`/check"`) {
		t.Error("book page links its rhyme check")
	}
	if resp, body := b.do("GET", "/books/"+id+"/check", "", nil); resp.StatusCode != 200 || !strings.Contains(body, "2 nhóm vần") {
		t.Errorf("book check: %d", resp.StatusCode)
	}
	if resp, _ := newBrowser(t, ts).do("GET", "/books/"+id+"/check", "", nil); resp.StatusCode != 404 {
		t.Errorf("private book: %d", resp.StatusCode)
	}
}

func TestRhymeCheckThrottle(t *testing.T) {
	var th throttle
	if !th.allow("u:1", time.Minute) || th.allow("u:1", time.Minute) || !th.allow("u:2", time.Minute) {
		t.Error("one event a minute per person")
	}
}
