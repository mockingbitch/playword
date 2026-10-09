package rhyme

import (
	"os"
	"slices"
	"testing"

	"playword/pkg/vnphon"
)

// These tests run the spec §7 / §1.8 acceptance cases against the real
// configs/ YAML: if tuning the YAML breaks one, the tuning changed product
// behaviour the spec promises.

func repoEngine(t testing.TB) *Engine {
	t.Helper()
	e, err := LoadDir("../../../configs")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func fixture(t testing.TB) []Candidate {
	t.Helper()
	f, err := os.Open("testdata/lexicon.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := ReadLexicon(f)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func search(t *testing.T, e *Engine, cands []Candidate, text string, mod func(*Query)) map[string]Result {
	t.Helper()
	p, err := vnphon.ParsePhrase(text)
	if err != nil {
		t.Fatal(err)
	}
	q := NewQuery(p)
	q.Limit = 1000
	if mod != nil {
		mod(&q)
	}
	resp, err := e.Search(q, cands)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Result{}
	for _, r := range resp.Results {
		out[r.Candidate.Text] = r
	}
	return out
}

func level(l Level) func(*Query) { return func(q *Query) { q.Level = l } }

func dialectLevel(d Dialect, l Level) func(*Query) {
	return func(q *Query) { q.Dialect, q.Level = d, l }
}

func TestSpecNgayTan(t *testing.T) {
	got := search(t, repoEngine(t), fixture(t), "ngày tàn", level(L25))
	for _, want := range []string{"chạy án", "chạy đạn", "may mắn"} {
		if r, ok := got[want]; !ok || r.Matched != 2 {
			t.Errorf("%q: got %+v, want a 2-syllable match", want, r)
		}
	}
	for _, not := range []string{"ngày mai", "ngày tàn"} {
		if _, ok := got[not]; ok {
			t.Errorf("%q must not be returned", not)
		}
	}
}

func TestSpecTanLevels(t *testing.T) {
	e, c := repoEngine(t), fixture(t)
	cases := []struct {
		l       Level
		word    string
		present bool
	}{
		{L0, "lan", false},
		{L1, "lan", true},
		{L1, "tán", false},
		{L2, "tán", true},
	}
	for _, tc := range cases {
		_, ok := search(t, e, c, "tàn", level(tc.l))[tc.word]
		if ok != tc.present {
			t.Errorf("tàn @%s: %q present=%v, want %v", tc.l, tc.word, ok, tc.present)
		}
	}
}

func TestSpecAnhSlant(t *testing.T) {
	got := search(t, repoEngine(t), fixture(t), "anh", level(L3))
	if _, ok := got["ăn"]; !ok {
		t.Error(`anh @L3 must contain "ăn"`)
	}
	if _, ok := got["ang"]; !ok {
		t.Error(`anh @L3 must contain "ang"`)
	}
}

func TestSpecDialects(t *testing.T) {
	e, c := repoEngine(t), fixture(t)
	cases := []struct {
		q, word string
		d       Dialect
		l       Level
		present bool
	}{
		{"tàn", "tàng", Nam, L0, true},
		{"tàn", "tàng", Bac, L0, false},
		{"bảo", "bão", Nam, L0, true},
		{"bảo", "bão", Bac, L0, false},
		{"bảo", "bão", Bac, L1, true},
	}
	for _, tc := range cases {
		_, ok := search(t, e, c, tc.q, dialectLevel(tc.d, tc.l))[tc.word]
		if ok != tc.present {
			t.Errorf("%s/%s @%s: %q present=%v, want %v", tc.q, tc.d, tc.l, tc.word, ok, tc.present)
		}
	}
}

func TestDialectMapIdempotent(t *testing.T) {
	e := repoEngine(t)
	for _, d := range Dialects {
		for _, w := range vnphon.RimeSpellings() {
			for _, onset := range []string{"", "t"} {
				for _, tone := range []string{"", "́", "̣"} {
					s, err := vnphon.ParseSyllable(onset + w + tone)
					if err != nil {
						continue
					}
					once := e.Map(d, []vnphon.Syllable{s})
					if twice := e.Map(d, once); !slices.Equal(once, twice) {
						t.Errorf("%s: %q not idempotent: %+v → %+v", d, onset+w+tone, once, twice)
					}
				}
			}
		}
	}
}

// Every candidate the fixture holds must round-trip (spec §7 property test).
func TestFixtureRoundTrip(t *testing.T) {
	for _, c := range fixture(t) {
		text, err := vnphon.RenderPhrase(c.Syllables)
		if err != nil {
			t.Fatalf("%q: %v", c.Text, err)
		}
		back, err := vnphon.ParsePhrase(text)
		if err != nil || !slices.Equal(back, c.Syllables) {
			t.Errorf("%q → %q → %+v, %v", c.Text, text, back, err)
		}
	}
}

// Product decision 2026-10-08: n ~ ng is a cross pair, so "chạy làng"
// rhymes with "ngày tàn" at the default level.
func TestDecisionNNgCross(t *testing.T) {
	got := search(t, repoEngine(t), fixture(t), "ngày tàn", level(L25))
	if r, ok := got["chạy làng"]; !ok || r.Matched != 2 || r.Level != L25 {
		t.Errorf(`"chạy làng": got %+v, want a 2-syllable L25 match`, r)
	}
}
