package rhyme

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"playword/pkg/vnphon"
)

// rankCands builds candidates from phrases with a Zipf frequency each; the
// frequent loose rhymes are there to prove frequency no longer beats
// closeness.
func rankCands(t testing.TB, words map[string]float64) []Candidate {
	t.Helper()
	var out []Candidate
	keys := make([]string, 0, len(words))
	for w := range words {
		keys = append(keys, w)
	}
	slices.Sort(keys)
	for i, w := range keys {
		p, err := vnphon.ParsePhrase(w)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, Candidate{ID: int64(i + 1), Text: w, Syllables: p, Zipf: words[w]})
	}
	return out
}

func ranked(t testing.TB, e *Engine, query string, l Level, cands []Candidate) []string {
	t.Helper()
	p, err := vnphon.ParsePhrase(query)
	if err != nil {
		t.Fatal(err)
	}
	q := NewQuery(p)
	q.Level, q.Limit = l, 100
	resp, err := e.Search(q, cands)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(resp.Results))
	for i, r := range resp.Results {
		out[i] = r.Candidate.Text
	}
	return out
}

func before(t *testing.T, list []string, a, b string) {
	t.Helper()
	i, j := slices.Index(list, a), slices.Index(list, b)
	if i < 0 || j < 0 || i > j {
		t.Errorf("%q must rank before %q: %v", a, b, list)
	}
}

func TestRankingCloseness(t *testing.T) {
	e := repoEngine(t)
	// Real words; the loose ones are frequent, the close ones rare.
	cands := rankCands(t, map[string]float64{
		"dày dạn": 2.0, "chạy án": 2.0, "hải quân": 6.5, "tài sản": 4.5, "ngày càng": 6.5, "giai đoạn": 6.5,
		"tàng": 2.0, "tần": 6.0, "tán": 2.0, "toàn": 2.0, "bàn": 2.0, "đàn": 6.0,
	})
	two := ranked(t, e, "ngày tàn", L25, cands)
	// a stricter level is never below a looser one, whatever the frequency
	before(t, two, "dày dạn", "hải quân")
	before(t, two, "chạy án", "giai đoạn")
	// inside rap-flex the closer rhyme wins: tài sản differs once (ai/ay),
	// hải quân twice (ai/ay and ân/an)
	before(t, two, "tài sản", "hải quân")
	// a result that repeats the query's own word ("ngày") comes after every
	// other two-syllable rhyme (decided 2026-10-21)
	before(t, two, "hải quân", "ngày càng")
	before(t, two, "giai đoạn", "ngày càng")

	one := ranked(t, e, "tàn", L25, cands)
	before(t, one, "bàn", "toàn") // same rime; toàn adds a medial
	before(t, one, "toàn", "tán") // a medial is closer than a tone
	before(t, one, "tán", "tàng") // a tone is closer than a coda
	// a coda change (n→ng) is closer than a vowel change (a→â), decided 2026-10-21
	before(t, one, "tàng", "tần")
	// frequency still breaks ties: đàn (6.0) and bàn (2.0) are equally close
	before(t, one, "đàn", "bàn")
}

func TestRankingEnglishCloseness(t *testing.T) {
	e := repoEngine(t)
	cands := []Candidate{
		enCand(t, 1, "honey", "HH AH1 N IY0"), enCand(t, 2, "tummy", "T AH1 M IY0"),
		enCand(t, 3, "company", "K AH1 M P AH0 N IY0"), enCand(t, 4, "money", "M AH1 N IY0"),
	}
	q := Query{Lang: "en", Segs: cands[3].Segs, Text: "money", Level: Family, MinSyl: 1, MaxSyl: MaxSyl, Match: AlignSuffix, Limit: 10}
	resp, err := e.Search(q, cands)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range resp.Results {
		got = append(got, r.Candidate.Text)
	}
	if strings.Join(got, " ") != "honey tummy company" {
		t.Errorf("english closeness: %v", got)
	}
}

// FuzzRankingFilters: whatever the query and the filters, every result
// satisfies them, the order follows the tiers, and it is deterministic.
func FuzzRankingFilters(f *testing.F) {
	f.Add(uint64(1), uint8(3), uint8(1), uint8(5), uint8(0), false, false)
	f.Add(uint64(42), uint8(0), uint8(2), uint8(2), uint8(2), true, true)
	e := repoEngine(f)
	cands := fixture(f)
	f.Fuzz(func(t *testing.T, seed uint64, level, minSyl, maxSyl, align uint8, noSlang, noProper bool) {
		r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		qc := cands[r.IntN(len(cands))]
		q := NewQuery(qc.Syllables)
		q.Level = Levels[int(level)%len(Levels)]
		q.MinSyl = 1 + int(minSyl)%MaxSyl
		q.MaxSyl = max(q.MinSyl, 1+int(maxSyl)%MaxSyl)
		q.Match = []Align{AlignSuffix, AlignPrefix, AlignAny}[int(align)%3]
		q.ExcludeSlang, q.ExcludeProper = noSlang, noProper
		q.Limit = 200
		resp, err := e.Search(q, cands)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := e.Search(q, cands)
		// the lazy top K (closeness only for contenders) must equal the head
		// of the complete ranking
		small := q
		small.Limit = 1 + int(seed%7)
		head, _ := e.Search(small, cands)
		for i, res := range head.Results {
			if i >= len(resp.Results) || resp.Results[i].Candidate.ID != res.Candidate.ID || resp.Results[i].Distance != res.Distance {
				t.Fatalf("top %d differs from the full ranking at %d", small.Limit, i)
			}
		}
		for i, res := range resp.Results {
			c := res.Candidate
			if res.Level > q.Level || res.Matched < q.MinSyl || res.Matched > q.MaxSyl ||
				(q.ExcludeSlang && c.IsSlang) || (q.ExcludeProper && c.IsProper) || res.End-res.Start != res.Matched {
				t.Fatalf("filter violated by %+v for %+v", res, q)
			}
			lo, hi := windowRange(len(c.Syllables), res.Matched, q.Match)
			if res.Start < lo || res.Start > hi {
				t.Fatalf("window %d outside %d..%d for %s", res.Start, lo, hi, q.Match)
			}
			if i > 0 && better(resp.Results[i-1], res) > 0 {
				t.Fatalf("out of order at %d", i)
			}
			if again.Results[i].Candidate.ID != c.ID {
				t.Fatal("order is not deterministic")
			}
		}
	})
}

// TestExplainMatchesRanking: the per-syllable explanation adds up to the
// distance and the repeated words the ranking used, for every result.
func TestExplainMatchesRanking(t *testing.T) {
	e := repoEngine(t)
	cands := fixture(t)
	for _, d := range Dialects {
		for _, qc := range cands[:min(len(cands), 40)] {
			q := NewQuery(qc.Syllables)
			q.Dialect, q.Level, q.Limit = d, L3, 200
			resp, err := e.Search(q, cands)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range resp.Results {
				pairs := e.Explain(resp.Syllables, d, r.Candidate.Syllables, r.Start, r.End)
				dist, ident := 0, 0
				for _, p := range pairs {
					dist += p.Diff.Distance()
					if p.Identical {
						ident++
					}
				}
				if len(pairs) != r.Matched || dist != int(r.Distance) || ident != int(r.Identical) {
					t.Fatalf("%s/%s ~ %s: explain %d pairs, distance %d, identical %d; ranking %d, %d, %d",
						d, qc.Text, r.Candidate.Text, len(pairs), dist, ident, r.Matched, r.Distance, r.Identical)
				}
			}
		}
	}
}

func TestExplainDiffs(t *testing.T) {
	e := repoEngine(t)
	parse := func(s string) []vnphon.Syllable {
		p, err := vnphon.ParsePhrase(s)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, tc := range []struct {
		query, cand string
		want        []Diff
	}{
		{"ngày tàn", "dày dạn", []Diff{0, DiffToneGroup}},
		{"ngày tàn", "hải quân", []Diff{DiffNucleus | DiffToneGroup, DiffNucleus | DiffMedial | DiffTone}},
		{"tàn", "tàng", []Diff{DiffCoda}},
		{"tàn", "toàn", []Diff{DiffMedial}},
	} {
		c := parse(tc.cand)
		got := e.Explain(parse(tc.query), Bac, c, 0, len(c))
		var diffs []Diff
		for _, p := range got {
			diffs = append(diffs, p.Diff)
		}
		if !slices.Equal(diffs, tc.want) {
			t.Errorf("%s ~ %s: diffs %b, want %b", tc.query, tc.cand, diffs, tc.want)
		}
	}
}

// TestDiffMatchesDistance: the explanation's parts weigh exactly what the
// ranking distance adds, for every pair of syllables that rhyme.
func TestDiffMatchesDistance(t *testing.T) {
	e := repoEngine(t)
	seen := map[Code]bool{}
	var codes []Code
	for _, c := range fixture(t) {
		for _, x := range e.Codes(c.Syllables) {
			if !seen[x] {
				seen[x] = true
				codes = append(codes, x)
			}
		}
	}
	pairs := 0
	for _, a := range codes {
		for _, b := range codes {
			if _, ok := e.codes.codeLevel(a, b); !ok {
				continue
			}
			pairs++
			if got, want := e.codes.sylDiff(a, b).Distance(), e.codes.sylDistance(a, b); got != want {
				t.Fatalf("%v ~ %v: diff weighs %d, distance %d", a, b, got, want)
			}
		}
	}
	if pairs < 100 {
		t.Fatalf("only %d rhyming pairs checked", pairs)
	}
}
