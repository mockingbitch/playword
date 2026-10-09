// Package lookup is the dictionary index: exact lookup, autocomplete that
// tolerates missing diacritics, telex and typos, and reverse search in
// definitions.
package lookup

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"playword/internal/adapters/index/csr"
	"playword/internal/domain/entry"
)

// Entry is one dictionary headword as the lookup index sees it.
type Entry struct {
	ID     int64
	Word   string
	Freq   float64 // Zipf; higher = more common, drives ranking
	Senses []entry.Sense
}

// Gloss is the first definition, used as a one-line preview in suggestions.
func (e *Entry) Gloss() string {
	for _, s := range e.Senses {
		if s.Gloss != "" {
			return s.Gloss
		}
	}
	return ""
}

type keyKind uint8

const (
	kindExact  keyKind = iota // normalized headword: "nhà"
	kindFolded                // without diacritics: "nha"
	kindTelex                 // as typed in telex: "nhaf"
)

type key struct {
	s    string
	id   int32
	kind keyKind
}

// Match describes how a suggestion matched the query, best first.
type Match uint8

// Match kinds, best first.
const (
	MatchExact Match = iota
	MatchPrefix
	MatchLoose // ignored diacritics or telex
	MatchFuzzy // typo-tolerant
)

// Suggestion is one autocomplete result and how it matched.
type Suggestion struct {
	Entry *Entry
	Match Match
	HL    int // leading runes of Entry.Word that matched the query (0 = none)
	rank  int
}

// maxFuzzyRunes bounds which headwords get typo-tolerance entries; longer
// phrases are found by prefix instead and would only bloat the delete map.
const maxFuzzyRunes = 24

// MaxSuggest caps the limit callers may ask for.
const MaxSuggest = 50

// Queries this short match a large share of the dictionary, so their ranked
// results are memoized; there are only a few thousand such prefixes.
const shortQueryRunes = 2

// Index is immutable once built, so any number of goroutines may query it
// without locking. Swap in a new Index to reload data.
type Index struct {
	entries  []Entry
	byWord   map[string]int32
	keys     []key              // sorted by s, for prefix scans
	deletes  *csr.Table         // folded headword and its 1-deletions -> ids (hashed; fuzzy verifies)
	postings map[string][]int32 // folded token in meanings -> ids
	short    sync.Map           // short normalized query -> []Suggestion (MaxSuggest long)
}

// NewIndex builds the lookup index over entries.
func NewIndex(entries []Entry) *Index {
	idx := &Index{
		entries:  entries,
		byWord:   make(map[string]int32, len(entries)),
		keys:     make([]key, 0, len(entries)*2),
		postings: make(map[string][]int32),
	}
	dels := csr.NewBuilder(csr.NewSeed())
	for i := range entries {
		id := int32(i)
		e := &entries[i]
		e.Word = strings.TrimSpace(e.Word)
		w := Normalize(e.Word)
		if _, dup := idx.byWord[w]; !dup {
			idx.byWord[w] = id
		}

		f, t := Fold(w), Telex(w)
		idx.keys = append(idx.keys, key{w, id, kindExact})
		if f != w {
			idx.keys = append(idx.keys, key{f, id, kindFolded})
		}
		if t != f {
			idx.keys = append(idx.keys, key{t, id, kindTelex})
		}

		if fr := []rune(f); len(fr) <= maxFuzzyRunes {
			for _, d := range deletions(fr) {
				dels.Add(d, id)
			}
		}

		for _, s := range e.Senses {
			for _, tok := range tokenize(s.Gloss) {
				idx.postings[tok] = appendUnique(idx.postings[tok], id)
			}
		}
	}
	idx.deletes = dels.Build()
	sort.Slice(idx.keys, func(a, b int) bool { return idx.keys[a].s < idx.keys[b].s })
	return idx
}

// Len is the number of entries.
func (idx *Index) Len() int { return len(idx.entries) }

// Lookup returns the entry whose headword equals word, ignoring case and
// Unicode normalization differences.
func (idx *Index) Lookup(word string) *Entry {
	if id, ok := idx.byWord[Normalize(word)]; ok {
		return &idx.entries[id]
	}
	return nil
}

// Suggest ranks headwords for a partially typed query: exact match, then
// prefix, then diacritic-insensitive / telex, then typos. Ties go to the more
// frequent, then the shorter word.
func (idx *Index) Suggest(query string, limit int) []Suggestion {
	q := Normalize(query)
	if q == "" || limit <= 0 {
		return nil
	}
	limit = min(limit, MaxSuggest)
	qLen := utf8.RuneCountInString(q)
	if qLen > shortQueryRunes {
		return idx.suggest(q, qLen, limit)
	}
	v, ok := idx.short.Load(q)
	if !ok {
		v, _ = idx.short.LoadOrStore(q, idx.suggest(q, qLen, MaxSuggest))
	}
	all := v.([]Suggestion)
	return slices.Clone(all[:min(limit, len(all))])
}

func (idx *Index) suggest(q string, qLen, limit int) []Suggestion {
	qf := Fold(q)

	type cand struct {
		rank int
		hl   int
	}
	best := make(map[int32]cand)
	add := func(id int32, rank, hl int) {
		if c, ok := best[id]; !ok || rank < c.rank {
			best[id] = cand{rank, hl}
		}
	}

	idx.scanPrefix(q, func(k key) {
		hl := qLen
		if k.kind == kindTelex {
			hl = 0 // telex spelling doesn't line up rune-for-rune with the word
		}
		switch {
		case k.s == q && k.kind == kindExact:
			add(k.id, 0, hl)
		case k.s == q:
			add(k.id, 1, hl)
		case k.kind == kindExact:
			add(k.id, 2, hl)
		default:
			add(k.id, 3, hl)
		}
	})
	// The user typed diacritics but maybe the wrong ones ("nhá" for "nhà").
	if qf != q {
		idx.scanPrefix(qf, func(k key) {
			if k.kind == kindFolded {
				add(k.id, 4, qLen)
			}
		})
	}
	if len(best) < limit {
		// Dropped letters ("helo" -> "hello") are the most common typo, so
		// they outrank substitutions at the same distance ("helo" -> "help").
		idx.fuzzy(qf, func(id int32, d int) {
			rank := 4 + 2*d
			if isSubsequence([]rune(qf), []rune(Fold(idx.entries[id].Word))) {
				rank--
			}
			add(id, rank, 0)
		})
	}

	out := make([]Suggestion, 0, len(best))
	for id, c := range best {
		out = append(out, Suggestion{Entry: &idx.entries[id], Match: matchOf(c.rank), HL: c.hl, rank: c.rank})
	}
	slices.SortFunc(out, func(a, b Suggestion) int {
		if d := a.rank - b.rank; d != 0 {
			return d
		}
		if a.Entry.Freq != b.Entry.Freq {
			if a.Entry.Freq > b.Entry.Freq {
				return -1
			}
			return 1
		}
		if d := len(a.Entry.Word) - len(b.Entry.Word); d != 0 {
			return d
		}
		return strings.Compare(a.Entry.Word, b.Entry.Word)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// SearchMeanings finds entries whose meanings contain every word of query
// (diacritics ignored) — the reverse lookup, e.g. "ngôi nhà" -> "house".
func (idx *Index) SearchMeanings(query string, limit int) []*Entry {
	toks := tokenize(query)
	if len(toks) == 0 {
		return nil
	}
	var ids []int32
	for i, t := range toks {
		p := idx.postings[t]
		if i == 0 {
			ids = slices.Clone(p)
		} else {
			ids = slices.DeleteFunc(ids, func(id int32) bool {
				_, found := slices.BinarySearch(p, id)
				return !found
			})
		}
		if len(ids) == 0 {
			return nil
		}
	}
	out := make([]*Entry, len(ids))
	for i, id := range ids {
		out[i] = &idx.entries[id]
	}
	slices.SortStableFunc(out, func(a, b *Entry) int {
		switch {
		case a.Freq > b.Freq:
			return -1
		case a.Freq < b.Freq:
			return 1
		}
		return 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (idx *Index) scanPrefix(p string, fn func(key)) {
	i := sort.Search(len(idx.keys), func(i int) bool { return idx.keys[i].s >= p })
	for ; i < len(idx.keys) && strings.HasPrefix(idx.keys[i].s, p); i++ {
		fn(idx.keys[i])
	}
}

// fuzzy finds headwords within edit distance 1 (short queries) or 2 of the
// folded query using symmetric delete lookup, then verifies the distance.
func (idx *Index) fuzzy(qf string, fn func(id int32, dist int)) {
	qr := []rune(qf)
	if len(qr) < 3 || len(qr) > maxFuzzyRunes {
		return
	}
	maxDist := 1
	if len(qr) >= 6 {
		maxDist = 2
	}
	seen := make(map[int32]bool)
	for _, d := range deletions(qr) {
		for _, id := range idx.deletes.Get(d) {
			if seen[id] {
				continue
			}
			seen[id] = true
			w := []rune(Fold(idx.entries[id].Word))
			if dist := distance(qr, w); dist > 0 && dist <= maxDist {
				fn(id, dist)
			}
		}
	}
}

func isSubsequence(sub, s []rune) bool {
	i := 0
	for _, r := range s {
		if i < len(sub) && sub[i] == r {
			i++
		}
	}
	return i == len(sub)
}

// deletions returns s itself plus every string with one rune removed.
func deletions(s []rune) []string {
	out := make([]string, 0, len(s)+1)
	out = append(out, string(s))
	for i := range s {
		out = append(out, string(s[:i])+string(s[i+1:]))
	}
	return out
}

// appendUnique relies on ids being added in increasing order, which keeps
// every posting list sorted for binary search.
func appendUnique(ids []int32, id int32) []int32 {
	if n := len(ids); n > 0 && ids[n-1] == id {
		return ids
	}
	return append(ids, id)
}

func matchOf(rank int) Match {
	switch {
	case rank == 0:
		return MatchExact
	case rank == 2:
		return MatchPrefix
	case rank <= 4:
		return MatchLoose
	}
	return MatchFuzzy
}

// Fuzzy reports a typo-tolerant match.
func (s Suggestion) Fuzzy() bool { return s.Match == MatchFuzzy }
