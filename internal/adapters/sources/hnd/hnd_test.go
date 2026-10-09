package hnd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"playword/internal/domain/entry"
)

func parseFile(t *testing.T) []entry.Entry {
	t.Helper()
	f, err := os.Open("testdata/sample.tab")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []entry.Entry
	err = New("", nil).Parse(context.Background(), f, func(e entry.Entry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParse(t *testing.T) {
	got := parseFile(t)
	byWord := map[string]entry.Entry{}
	for _, e := range got {
		byWord[e.Headword] = e
	}
	if len(got) != 9 || byWord["a hoàn"].Headword == "" {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}

	may := byWord["may mắn"]
	if len(may.Senses) != 1 || may.Senses[0].Gloss != "Gặp dịp tốt." || may.Lang != entry.VI {
		t.Errorf("may mắn = %+v (hidden unaccented text must be removed)", may)
	}

	an := byWord["an"]
	if an.POS != "tính từ" || an.Senses[0].Gloss != "Yên, yên ổn" || an.Senses[0].Examples[0] != "tình hình lúc an lúc nguy" {
		t.Errorf("an = %+v", an)
	}

	// Tone variants share a line; each <h3> is its own entry.
	án := byWord["án"]
	if len(án.Senses) != 3 || án.Senses[2].POS != "động từ" || án.Senses[0].Gloss != "Bàn cao và hẹp mặt." {
		t.Errorf("án = %+v", án)
	}

	anh := byWord["anh"]
	if len(anh.Senses) != 2 || anh.Senses[1].POS != "đại từ" || anh.Senses[1].Gloss != "Ngôi thứ nhất & thân mật" {
		t.Errorf("anh = %+v (// switches part of speech; dangling <font is dropped)", anh)
	}

	// Etymology notes contain colons; they must not split off an example.
	ao := byWord["ai oán"]
	if ao.POS != "động từ" || ao.Senses[0].Gloss != "Đau thương oán trách (H. ai: thương xót; oán: hờn giận)" ||
		ao.Senses[0].Examples[0] != "Hơn thiệt thôi đừng ai oán nữa" {
		t.Errorf("ai oán = %+v", ao)
	}
	if gd := byWord["giai đoạn"]; gd.POS != "danh từ" || gd.Senses[0].Gloss != "Phần thời gian (H. giai: bậc; đoạn: phân chia)" {
		t.Errorf("POS without a dot before an etymology: %+v", gd)
	}

	// Numbered sub-senses become senses; "3 người" (lowercase) is not a number.
	nha := byWord["nhà"]
	if len(nha.Senses) != 2 || nha.Senses[1].Gloss != "Chỗ ở của một gia đình" ||
		nha.Senses[1].Examples[0] != "dọn đến nhà mới 3 người không phải số mục" || nha.Senses[0].Examples[0] != "xây dựng nhà ở" {
		t.Errorf("nhà = %+v", nha)
	}
	if án := byWord["án"]; len(án.Senses) != 3 {
		t.Errorf("án = %+v", án)
	}

	if nh := byWord["Nh"]; nh.POS != "" || nh.Senses[0].Gloss != "Nh. Như." {
		t.Errorf("Nh. is not a part of speech: %+v", nh)
	}
}

func TestParseStopsOnEmitError(t *testing.T) {
	f, _ := os.Open("testdata/sample.tab")
	defer f.Close()
	stop := errors.New("stop")
	err := New("", nil).Parse(context.Background(), f, func(entry.Entry) error { return stop })
	if !errors.Is(err, stop) {
		t.Errorf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f2, _ := os.Open("testdata/sample.tab")
	defer f2.Close()
	if err := New("", nil).Parse(ctx, f2, func(entry.Entry) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: %v", err)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/missing":
			http.NotFound(w, r)
		case r.Header.Get("If-Modified-Since") != "":
			w.WriteHeader(http.StatusNotModified)
		default:
			w.Header().Set("ETag", `"v1"`)
			io.WriteString(w, "may mắn\t- Gặp dịp tốt.\n")
		}
	}))
	defer srv.Close()

	s := New(srv.URL+"/dict.tab", srv.Client())
	if s.Name() != "hnd" || s.Kind() != "dictionary" || s.Lang() != entry.VI || s.License() == "" || s.URL() == "" {
		t.Error("metadata")
	}
	rc, meta, err := s.Fetch(context.Background(), time.Time{})
	if err != nil || meta.ETag != `"v1"` {
		t.Fatalf("fetch: %v %+v", err, meta)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if len(b) == 0 {
		t.Error("empty body")
	}
	if _, meta, err := s.Fetch(context.Background(), time.Now()); err != nil || !meta.NotModified {
		t.Errorf("not modified: %v %+v", err, meta)
	}
	if _, _, err := New(srv.URL+"/missing", nil).Fetch(context.Background(), time.Time{}); err == nil {
		t.Error("404 must fail")
	}
	if New("", nil).URL() != DefaultURL {
		t.Error("default URL")
	}
}
