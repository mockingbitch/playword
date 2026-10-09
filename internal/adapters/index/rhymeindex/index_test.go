package rhymeindex

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/ports"
	"playword/pkg/vnphon"
)

func engine(t testing.TB) *rhyme.Engine {
	t.Helper()
	e, err := rhyme.LoadDir("../../../../configs")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// syntheticData builds n entries of 1–4 random but valid syllables.
func syntheticData(_ testing.TB, e *rhyme.Engine, n int) *ports.IndexData {
	r := rand.New(rand.NewPCG(7, 7))
	var pool []string
	for _, sp := range vnphon.RimeSpellings() {
		for _, o := range []string{"", "b", "ch", "đ", "h", "l", "m", "ng", "nh", "t", "th", "tr", "v", "x"} {
			for _, tone := range []string{"", "̀", "́", "̉", "̃", "̣"} {
				if _, err := vnphon.ParseSyllable(o + sp + tone); err == nil {
					pool = append(pool, o+sp+tone)
				}
			}
		}
	}
	d := &ports.IndexData{ConfigID: e.ConfigID(), Syllables: map[int64][]vnphon.Syllable{}, Senses: map[int64][]entry.Sense{}}
	seen := map[string]bool{}
	for id := int64(1); len(d.Entries) < n; id++ {
		k := 1 + r.IntN(4)
		words := make([]string, k)
		for i := range words {
			words[i] = pool[r.IntN(len(pool))]
		}
		text := fmt.Sprint(words)
		text = text[1 : len(text)-1]
		if seen[text] {
			continue
		}
		seen[text] = true
		syl, _ := vnphon.ParsePhrase(text)
		d.Entries = append(d.Entries, ports.IndexEntry{ID: id, Headword: text, Zipf: r.Float64() * 6})
		d.Syllables[id] = syl
		d.Senses[id] = []entry.Sense{{Gloss: "nghĩa thử của " + text}}
	}
	return d
}

func TestBuildAndSearch(t *testing.T) {
	e := engine(t)
	d := &ports.IndexData{ConfigID: e.ConfigID(), Syllables: map[int64][]vnphon.Syllable{}, Senses: map[int64][]entry.Sense{}}
	for i, w := range []string{"may mắn", "chạy án", "ngày mai", "wifi", "trống"} {
		id := int64(i + 1)
		d.Entries = append(d.Entries, ports.IndexEntry{ID: id, Headword: w})
		if w != "trống" {
			d.Senses[id] = []entry.Sense{{Gloss: "x"}}
		}
		if syl, err := vnphon.ParsePhrase(w); err == nil {
			d.Syllables[id] = syl
		}
	}
	s, err := Build(d, e)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 4 || s.Rhymable() != 3 || s.Entry(5) != nil || !s.Entry(4).RhymeError {
		t.Fatalf("len=%d rhymable=%d", s.Len(), s.Rhymable())
	}
	q := rhyme.NewQuery(mustParse(t, "ngày tàn"))
	resp, err := s.Search(e, q)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range resp.Results {
		got = append(got, r.Candidate.Text)
	}
	if !slices.Equal(got, []string{"chạy án", "may mắn"}) { // chạy án matches at L2, may mắn at L2.5
		t.Errorf("results = %v", got)
	}
	if s.Dictionary(entry.VI).Lookup("WIFI") == nil {
		t.Error("non-rhymable entries stay in the dictionary")
	}

	d.ConfigID = "other"
	if _, err := Build(d, e); !errors.Is(err, ports.ErrKeysConfigMismatch) {
		t.Errorf("config mismatch: %v", err)
	}
}

// The index must return exactly what the reference (full scan) returns.
func TestIndexMatchesReference(t *testing.T) {
	e := engine(t)
	d := syntheticData(t, e, 3000)
	s, err := Build(d, e)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 200; i++ {
		c := s.langs[entry.VI].cands[r.IntN(len(s.langs[entry.VI].cands))]
		q := rhyme.NewQuery(c.Syllables)
		q.Level = rhyme.Levels[r.IntN(len(rhyme.Levels))]
		q.Dialect = rhyme.Dialects[r.IntN(3)]
		q.Match = []rhyme.Align{rhyme.AlignSuffix, rhyme.AlignPrefix, rhyme.AlignAny}[r.IntN(3)]
		q.Limit = 10_000
		fast, err1 := s.Search(e, q)
		ref, err2 := e.Search(q, s.langs[entry.VI].cands)
		if err1 != nil || err2 != nil || len(fast.Results) != len(ref.Results) {
			t.Fatalf("query %q %+v: index %d results, reference %d", c.Text, q, len(fast.Results), len(ref.Results))
		}
		for j := range ref.Results {
			if fast.Results[j].Candidate.ID != ref.Results[j].Candidate.ID {
				t.Fatalf("query %q: result %d differs", c.Text, j)
			}
		}
	}
}

type fakeLoader struct {
	d   *ports.IndexData
	err error
}

func (f fakeLoader) LoadIndex(context.Context) (*ports.IndexData, error) {
	return f.d, f.err
}

func TestHolder(t *testing.T) {
	e := engine(t)
	var h Holder
	if h.Load() != nil {
		t.Fatal("empty holder")
	}
	d := syntheticData(t, e, 50)
	s1, err := h.Rebuild(context.Background(), fakeLoader{d: d}, e)
	if err != nil || h.Load() != s1 || s1.Gen != 1 {
		t.Fatalf("first build: %v", err)
	}
	if _, err := h.Rebuild(context.Background(), fakeLoader{err: errors.New("db down")}, e); err == nil || h.Load() != s1 {
		t.Error("a failed rebuild must keep the old snapshot")
	}
	bad := *d
	bad.ConfigID = "x"
	if _, err := h.Rebuild(context.Background(), fakeLoader{d: &bad}, e); err == nil || h.Load() != s1 {
		t.Error("a mismatched config must keep the old snapshot")
	}
	if s2, _ := h.Rebuild(context.Background(), fakeLoader{d: d}, e); s2.Gen != 2 {
		t.Error("generation must grow")
	}
}

func mustParse(t testing.TB, s string) []vnphon.Syllable {
	t.Helper()
	p, err := vnphon.ParsePhrase(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var bench struct {
	s *Snapshot
	e *rhyme.Engine
}

// benchSnapshot builds the synthetic index once (500k entries, or
// PLAYWORD_BENCH_ENTRIES — CI uses fewer) and reports its live heap
// (ADR-002 §6 target: < 1 GB at 500k).
func benchSnapshot(b *testing.B) (*Snapshot, *rhyme.Engine) {
	if bench.s == nil {
		e := engine(b)
		n := 500_000
		if v, err := strconv.Atoi(os.Getenv("PLAYWORD_BENCH_ENTRIES")); err == nil && v > 0 {
			n = v
		}
		d := syntheticData(b, e, n)
		var before runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		s, err := Build(d, e)
		if err != nil {
			b.Fatal(err)
		}
		runtime.KeepAlive(d) // d is garbage from here on
		var after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&after)
		b.Logf("%d entries: snapshot heap ≈ %d MB, rhyme key table %d MB (%d keys, %d postings)",
			n, (int64(after.HeapAlloc)-int64(before.HeapAlloc))>>20+snapshotDataMB(s), s.langs[entry.VI].byKey.Bytes()>>20, s.langs[entry.VI].byKey.Keys(), s.langs[entry.VI].byKey.Len())
		bench.s, bench.e = s, e
	}
	return bench.s, bench.e
}

// snapshotDataMB approximates the entry data that was already live before
// Build (it came from IndexData and is now owned by the snapshot).
func snapshotDataMB(s *Snapshot) int64 {
	var n int64
	for _, e := range s.entries {
		n += int64(len(e.Headword)) + 48*int64(len(e.Senses)) + 24*int64(len(e.Syllables))*3
		for _, sn := range e.Senses {
			n += int64(len(sn.Gloss))
		}
	}
	return n >> 20
}

func benchLookup(b *testing.B, syllables int) {
	s, e := benchSnapshot(b)
	var queries []rhyme.Query
	for _, c := range s.langs[entry.VI].cands {
		if len(c.Syllables) == syllables {
			queries = append(queries, rhyme.NewQuery(c.Syllables))
			if len(queries) == 1000 {
				break
			}
		}
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := s.Search(e, queries[i%len(queries)]); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

func BenchmarkLookup_1syl(b *testing.B) { benchLookup(b, 1) }
func BenchmarkLookup_2syl(b *testing.B) { benchLookup(b, 2) }
func BenchmarkLookup_3syl(b *testing.B) { benchLookup(b, 3) }

// BenchmarkLookupLatency is one query at a time: the latency a user sees on
// a cache miss, worst case (1 syllable, rap-flex) on 500k entries.
func BenchmarkLookupLatency_1syl(b *testing.B) {
	s, e := benchSnapshot(b)
	var queries []rhyme.Query
	for _, c := range s.langs[entry.VI].cands {
		if len(c.Syllables) == 1 {
			queries = append(queries, rhyme.NewQuery(c.Syllables))
			if len(queries) == 200 {
				break
			}
		}
	}
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		resp, _ := s.Search(e, queries[i%len(queries)])
		_ = resp
	}
}

func TestMergeSources(t *testing.T) {
	e := engine(t)
	d := &ports.IndexData{ConfigID: e.ConfigID(), Syllables: map[int64][]vnphon.Syllable{}, Senses: map[int64][]entry.Sense{}}
	add := func(id int64, word, source, gloss string, slang bool, trend float64) {
		d.Entries = append(d.Entries, ports.IndexEntry{ID: id, Lang: entry.VI, Headword: word, Normalized: entry.Normalize(word),
			Source: source, IsSlang: slang, Trend: trend})
		d.Senses[id] = []entry.Sense{{Gloss: gloss}}
		d.Syllables[id] = mustParse(t, word)
	}
	add(1, "cỏ", "wiktionary-vi", "Cây thân mềm.", false, 0) // lower-ranked source first in the data
	add(2, "cỏ", "hnd", "Loài cây thân mềm, mọc hoang", false, 0)
	add(3, "cỏ", "community", "Cần sa", true, 1)
	add(4, "may mắn", "hnd", "Gặp dịp tốt.", false, 0)
	add(5, "may mắn", "wiktionary-vi", "Gặp dịp tốt", false, 0) // same gloss, kept once
	s, err := Build(d, e)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 2 {
		t.Fatalf("entries = %d", s.Len())
	}
	co := s.Entry(1)
	if co != s.Entry(2) || co != s.Entry(3) || co.ID != 2 {
		t.Fatalf("every source id must reach the merged entry (primary hnd): %+v", co)
	}
	if co.Source != "hnd, wiktionary-vi, community" || !co.IsSlang || co.Trend != 1 || len(co.Senses) != 3 ||
		co.Senses[0].Gloss != "Loài cây thân mềm, mọc hoang" {
		t.Errorf("merged = %+v", co)
	}
	if mm := s.Entry(5); mm.ID != 4 || len(mm.Senses) != 1 {
		t.Errorf("duplicate gloss = %+v", mm)
	}
	resp, _ := s.Search(e, rhyme.NewQuery(mustParse(t, "ngày tàn")))
	n := 0
	for _, r := range resp.Results {
		if r.Candidate.Text == "may mắn" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("may mắn appears %d times", n)
	}
}
