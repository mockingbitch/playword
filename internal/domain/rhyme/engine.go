package rhyme

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme/en"
	"playword/pkg/vnphon"
)

// MaxSyl is the longest phrase stored and the longest query suffix matched.
const MaxSyl = 5

// ErrInvalidQuery is matched by every rejected query parameter.
var ErrInvalidQuery = errors.New("invalid rhyme query")

// Engine combines the keyer and dialect mappers built from one config.
type Engine struct {
	keyer   *Keyer
	mappers map[Dialect]DialectMapper

	codes *codeTables

	configID                   string
	familiesYAML, dialectsYAML string
}

// NewEngine validates and compiles both configs.
func NewEngine(f *FamiliesConfig, d *DialectsConfig) (*Engine, error) {
	k, err := NewKeyer(f)
	if err != nil {
		return nil, err
	}
	e := &Engine{keyer: k, mappers: map[Dialect]DialectMapper{}}
	for name, rules := range map[Dialect]DialectRules{Bac: d.Bac, Trung: d.Trung, Nam: d.Nam} {
		m, err := NewMapper(string(name), rules)
		if err != nil {
			return nil, err
		}
		e.mappers[name] = m
	}
	e.buildCodeTables()
	return e, nil
}

// Keyer returns the key builder.
func (e *Engine) Keyer() *Keyer { return e.keyer }

// Map applies the dialect to every syllable of p.
func (e *Engine) Map(d Dialect, p []vnphon.Syllable) []vnphon.Syllable {
	m := e.mappers[d]
	out := make([]vnphon.Syllable, len(p))
	for i, s := range p {
		out[i] = m.Map(s)
	}
	return out
}

// Keys returns the key of the whole phrase at every level for dialect d.
func (e *Engine) Keys(d Dialect, p []vnphon.Syllable) map[Level]string {
	mapped := e.Map(d, p)
	out := map[Level]string{}
	for _, l := range Levels {
		out[l] = e.keyer.PhraseKey(l, mapped)
	}
	return out
}

// matchLevel is the level of a multi-syllable match: that of its weakest
// syllable (ADR-001 §4, Q3).
func (e *Engine) matchLevel(q, w []Code) (Level, bool) {
	worst := L0
	for i := range q {
		l, ok := e.codes.codeLevel(q[i], w[i])
		if !ok {
			return 0, false
		}
		worst = max(worst, l)
	}
	return worst, true
}

// Align says where in a candidate the matched syllables may sit.
type Align string

// Alignments.
const (
	AlignSuffix Align = "suffix" // candidate ends with the matched syllables
	AlignPrefix Align = "prefix" // candidate starts with them
	AlignAny    Align = "any"    // anywhere in the candidate
)

// ParseAlign accepts suffix, prefix or any.
func ParseAlign(s string) (Align, error) {
	switch a := Align(s); a {
	case AlignSuffix, AlignPrefix, AlignAny:
		return a, nil
	}
	return "", fmt.Errorf("%w: unknown match %q", ErrInvalidQuery, s)
}

// Candidate is an entry that may rhyme.
type Candidate struct {
	ID        int64
	Text      string
	Syllables []vnphon.Syllable // standard orthography, not dialect-mapped
	Zipf      float64           // word frequency on the Zipf scale (0–8)
	Trend     float64           // already decayed, see DecayTrend
	IsSlang   bool
	IsProper  bool     // name of a person or place
	Codes     []Code   // optional: Engine.Codes(Syllables), precomputed by indexes
	Segs      []en.Seg // English entries: rhyme segments instead of Syllables
}

// Query is a rhyme search.
type Query struct {
	Lang          entry.Lang        // vi (default) or en
	Syllables     []vnphon.Syllable // vi
	Segs          []en.Seg          // en
	Text          string            // en: normalized query text, never returned as its own rhyme
	Dialect       Dialect           // vi only
	Level         Level             // loosest level accepted
	MinSyl        int
	MaxSyl        int
	Match         Align
	ExcludeSlang  bool
	ExcludeProper bool
	Limit         int
}

// NewQuery returns a query with the spec defaults: bac, L2.5, 1–5 syllables,
// suffix alignment, 50 results.
func NewQuery(p []vnphon.Syllable) Query {
	return Query{Syllables: p, Dialect: Bac, Level: L25, MinSyl: 1, MaxSyl: MaxSyl, Match: AlignSuffix, Limit: 50}
}

// Result is one ranked match.
type Result struct {
	Candidate *Candidate
	Matched   int // matched syllables
	Start     int // matched window in the candidate: Syllables[Start:End]
	End       int
	Score     float64
	// Level, Identical and Distance share one word: the top-K heap copies
	// Results around, so the struct stays at 48 bytes.
	Level     Level  // level of the weakest matched syllable
	Identical uint8  // matched syllables that are the query's own word (spelled the same)
	Distance  uint16 // phonetic distance inside the level, 0 for a perfect echo (closeness.go)
}

// Response holds the ranked results of one query.
type Response struct {
	Syllables []vnphon.Syllable // the query actually used (at most MaxSyl)
	Results   []Result
	Warnings  []string
}

func (q Query) validate() error {
	if q.Lang == entry.EN {
		switch {
		case len(q.Segs) == 0:
			return fmt.Errorf("%w: empty query", ErrInvalidQuery)
		case !q.Level.IsEnglish():
			return fmt.Errorf("%w: level %s is not an English level", ErrInvalidQuery, q.Level)
		case q.MinSyl < 1 || q.MaxSyl < q.MinSyl || q.MaxSyl > MaxSyl:
			return fmt.Errorf("%w: need 1 <= min_syl <= max_syl <= %d", ErrInvalidQuery, MaxSyl)
		case q.Limit < 1:
			return fmt.Errorf("%w: limit must be positive", ErrInvalidQuery)
		}
		_, err := ParseAlign(string(q.Match))
		return err
	}
	switch {
	case q.Level.IsEnglish():
		return fmt.Errorf("%w: level %s is not a Vietnamese level", ErrInvalidQuery, q.Level)
	case len(q.Syllables) == 0:
		return fmt.Errorf("%w: empty query", ErrInvalidQuery)
	case !slices.Contains(Dialects, q.Dialect):
		return fmt.Errorf("%w: unknown dialect %q", ErrInvalidQuery, q.Dialect)
	case q.Level > L3:
		return fmt.Errorf("%w: unknown level %d", ErrInvalidQuery, q.Level)
	case q.MinSyl < 1 || q.MaxSyl < q.MinSyl || q.MaxSyl > MaxSyl:
		return fmt.Errorf("%w: need 1 <= min_syl <= max_syl <= %d", ErrInvalidQuery, MaxSyl)
	case q.Limit < 1:
		return fmt.Errorf("%w: limit must be positive", ErrInvalidQuery)
	}
	_, err := ParseAlign(string(q.Match))
	return err
}

// Search is the reference implementation: it scans every candidate. The
// in-memory index (ADR-002) must return the same results, faster.
func (e *Engine) Search(q Query, cands []Candidate) (Response, error) {
	return e.SearchIn(q, cands, nil)
}

// SearchIn ranks only cands[i] for i in subset (all of cands when subset is
// nil), so an index can pass its candidate set without copying it.
func (e *Engine) SearchIn(q Query, cands []Candidate, subset []int32) (Response, error) {
	if err := q.validate(); err != nil {
		return Response{}, err
	}
	if q.Lang == entry.EN {
		return searchEN(q, cands, subset), nil
	}
	var resp Response
	used := q.Syllables
	if len(used) > MaxSyl {
		resp.Warnings = append(resp.Warnings,
			fmt.Sprintf("query has %d syllables; only the last %d are matched", len(used), MaxSyl))
		used = used[len(used)-MaxSyl:]
	}
	resp.Syllables = used
	dmap := &e.codes.dialect[dialectIndex(q.Dialect)]
	qu := e.Codes(used) // unmapped, for the identity check
	qc := make([]Code, len(qu))
	for i, c := range qu {
		qc[i] = dmap[c]
	}

	cc := make([]Code, 0, MaxSyl*3)
	n := len(cands)
	if subset != nil {
		n = len(subset)
	}
	top := newTopK(q.Limit)
	for k := range n {
		i := k
		if subset != nil {
			i = int(subset[k])
		}
		c := &cands[i]
		if (q.ExcludeSlang && c.IsSlang) || (q.ExcludeProper && c.IsProper) || slices.Equal(c.Syllables, q.Syllables) {
			continue
		}
		codes := c.Codes
		if codes == nil {
			codes = e.Codes(c.Syllables)
		}
		cc = cc[:0]
		for _, x := range codes {
			cc = append(cc, dmap[x])
		}
		if r, ok := e.match(qc, cc, q); ok {
			r.Candidate = c
			r.Score = Score(r.Matched, r.Level, c.Zipf, c.Trend, len(c.Syllables), len(used))
			// r is optimistic here (no repeated word, distance 0, the
			// strictest window): if even that cannot enter a full top K,
			// the real result cannot either, so skip computing closeness.
			if top.full() && better(r, top.worst()) >= 0 {
				continue
			}
			e.refine(&r, qc, cc, qu, codes, used, c.Syllables, q)
			// refine may pick another window (AlignAny), whose level sets the score
			r.Score = Score(r.Matched, r.Level, c.Zipf, c.Trend, len(c.Syllables), len(used))
			top.push(r)
		}
	}
	resp.Results = top.sorted()
	return resp, nil
}

// better is the result order, tier by tier (.docs/adr/ADR-004):
//
//  1. more matched syllables (ADR-001 §7: one more syllable always wins)
//  2. ordinary words before names of people and places (2026-10-08)
//  3. fewer syllables that repeat the query's own word (2026-10-21, Q14)
//  4. stricter level
//  5. smaller phonetic distance (closeness.go)
//  6. score: frequency, trend and length only break the ties left
//  7. text, then id, so the order is total and pages stay stable
//
// It is on the hot path, so later keys are only compared on ties.
func better(a, b Result) int {
	if a.Matched != b.Matched {
		return cmp.Compare(b.Matched, a.Matched)
	}
	if c := cmpBool(a.Candidate.IsProper, b.Candidate.IsProper); c != 0 {
		return c
	}
	if a.Identical != b.Identical {
		return cmp.Compare(a.Identical, b.Identical)
	}
	if a.Level != b.Level {
		return cmp.Compare(a.Level, b.Level)
	}
	if a.Distance != b.Distance {
		return cmp.Compare(a.Distance, b.Distance)
	}
	if a.Score != b.Score {
		return cmp.Compare(b.Score, a.Score)
	}
	if c := cmp.Compare(a.Candidate.Text, b.Candidate.Text); c != 0 {
		return c
	}
	return cmp.Compare(a.Candidate.ID, b.Candidate.ID)
}

// topK keeps the k best results in a heap whose root is the worst kept, so
// ranking n candidates costs O(n log k) instead of sorting all of them.
type topK struct {
	k int
	h []Result
}

func newTopK(k int) *topK { return &topK{k: k} }

func (t *topK) push(r Result) {
	if len(t.h) < t.k {
		t.h = append(t.h, r)
		for i := len(t.h) - 1; i > 0; {
			p := (i - 1) / 2
			if better(t.h[i], t.h[p]) <= 0 {
				break
			}
			t.h[i], t.h[p] = t.h[p], t.h[i]
			i = p
		}
		return
	}
	if better(r, t.h[0]) >= 0 {
		return
	}
	t.h[0] = r
	for i := 0; ; {
		worst, l, rr := i, 2*i+1, 2*i+2
		if l < len(t.h) && better(t.h[l], t.h[worst]) > 0 {
			worst = l
		}
		if rr < len(t.h) && better(t.h[rr], t.h[worst]) > 0 {
			worst = rr
		}
		if worst == i {
			return
		}
		t.h[i], t.h[worst] = t.h[worst], t.h[i]
		i = worst
	}
}

// full reports whether the heap holds k results; worst is then its root.
func (t *topK) full() bool { return len(t.h) == t.k }

func (t *topK) worst() Result { return t.h[0] }

func (t *topK) sorted() []Result {
	slices.SortFunc(t.h, better)
	return t.h
}

// cmpBool orders false before true.
func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// match finds the longest query suffix that rhymes with a window of the
// candidate allowed by q.Match, at q.Level or stricter; both sides are
// dialect-mapped codes. It ranks windows by level only: closeness is
// computed later, and only for results that can still enter the top K
// (see refine), because most matches never do.
func (e *Engine) match(qm, cm []Code, q Query) (Result, bool) {
	for n := min(len(qm), q.MaxSyl, len(cm)); n >= q.MinSyl; n-- {
		suffix := qm[len(qm)-n:]
		best, found := Result{}, false
		lo, hi := windowRange(len(cm), n, q.Match)
		for start := hi; start >= lo; start-- {
			l, ok := e.matchLevel(suffix, cm[start:start+n])
			if ok && l <= q.Level && (!found || l < best.Level) {
				best, found = Result{Matched: n, Level: l, Start: start, End: start + n}, true
			}
		}
		if found {
			return best, true
		}
	}
	return Result{}, false
}

// refine fills in the closeness of a match (qu, cu: unmapped codes; qs, cs:
// syllables). With AlignAny a candidate may hold several windows of the same
// length; refine keeps the one that ranks best — fewer repeated words, then
// stricter level, then closer — the nearest the end on ties.
func (e *Engine) refine(r *Result, qm, cm, qu, cu []Code, qs, cs []vnphon.Syllable, q Query) {
	n := r.Matched
	suffix, qusuf, qsuf := qm[len(qm)-n:], qu[len(qu)-n:], qs[max(len(qs)-n, 0):]
	lo, hi := r.Start, r.Start
	if q.Match == AlignAny {
		lo, hi = windowRange(len(cm), n, q.Match)
	}
	found := false
	best := *r
	for start := hi; start >= lo; start-- {
		l, ok := e.matchLevel(suffix, cm[start:start+n])
		if !ok || l > q.Level {
			continue
		}
		var cw []vnphon.Syllable
		if start+n <= len(cs) {
			cw = cs[start : start+n]
		}
		dist, ident := e.closeness(suffix, cm[start:start+n], qusuf, cu[start:start+n], qsuf, cw)
		w := Result{Matched: n, Level: l, Start: start, End: start + n, Identical: uint8(ident), Distance: uint16(dist)}
		if !found || closer(w, best) {
			best, found = w, true
		}
	}
	r.Level, r.Start, r.End, r.Identical, r.Distance = best.Level, best.Start, best.End, best.Identical, best.Distance
}

// windowRange gives the candidate start positions allowed for an
// n-syllable match; Search walks them from the end of the candidate (closest
// to where the line rhymes).
func windowRange(length, n int, a Align) (lo, hi int) {
	switch a {
	case AlignPrefix:
		return 0, 0
	case AlignAny:
		return 0, length - n
	}
	return length - n, length - n
}

var levelBonus = [...]float64{L0: 30, L1: 24, L2: 18, L25: 12, L3: 6, Perfect: 30, Family: 18, Assonance: 6}

// Bounds keep the spread of the secondary terms below 100 — from
// 6 (L3) − 20 (length) = −14 up to 30 + 35 + 20 = 85 — so one more matched
// syllable always outranks everything else (ADR-001 §7, Q7).
const (
	maxFreqBonus  = 35
	maxTrendBonus = 20
)

// Score implements spec §1.7 with the ADR-001 §7 bounds.
func Score(matched int, l Level, zipf, trend float64, candSyl, querySyl int) float64 {
	diff := candSyl - querySyl
	if diff < 0 {
		diff = -diff
	}
	return 100*float64(matched) +
		levelBonus[l] +
		math.Min(5*math.Max(zipf, 0), maxFreqBonus) +
		math.Min(15*math.Max(trend, 0), maxTrendBonus) -
		5*float64(diff)
}

// DecayTrend halves a slang trend score every 90 days.
func DecayTrend(score float64, age time.Duration) float64 {
	return score * math.Pow(0.5, age.Hours()/24/90)
}

// WindowKey is one stored rhyme key: the key of Syllables[Start:Start+N]
// at a level, in a dialect (ADR-003 D1).
type WindowKey struct {
	Dialect Dialect
	Level   Level
	Start   int
	N       int
	Key     string
}

// WindowKeys returns the keys of every contiguous window of up to MaxSyl
// syllables, for every dialect and level.
func (e *Engine) WindowKeys(p []vnphon.Syllable) []WindowKey {
	var out []WindowKey
	for _, d := range Dialects {
		mapped := e.Map(d, p)
		for start := range mapped {
			for n := 1; n <= MaxSyl && start+n <= len(mapped); n++ {
				for _, l := range Levels {
					out = append(out, WindowKey{d, l, start, n, e.keyer.PhraseKey(l, mapped[start:start+n])})
				}
			}
		}
	}
	return out
}

// ConfigID identifies the YAML that produced this engine's keys; it is empty
// for engines built from structs rather than files.
func (e *Engine) ConfigID() string { return e.configID }

// ConfigSource returns the raw YAML this engine was loaded from.
func (e *Engine) ConfigSource() (families, dialects string) { return e.familiesYAML, e.dialectsYAML }

// closer compares two windows of one candidate with the ranking tiers that
// can differ between them.
func closer(a, b Result) bool {
	if a.Identical != b.Identical {
		return a.Identical < b.Identical
	}
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	return a.Distance < b.Distance
}
