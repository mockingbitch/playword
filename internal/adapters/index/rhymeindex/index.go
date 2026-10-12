// Package rhymeindex is the in-memory index (ADR-002): an immutable
// Snapshot holding, per language, the rhyme key index and the dictionary
// lookup index, built from storage and swapped atomically (RCU).
package rhymeindex

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"playword/internal/adapters/index/csr"
	"playword/internal/adapters/index/lookup"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ports"
	"playword/pkg/vnphon"
)

// Entry is what a search result or dictionary page shows.
type Entry struct {
	ID         int64
	Lang       entry.Lang
	Headword   string
	POS        string
	Source     string
	IsSlang    bool
	Trend      float64
	Tags       []string
	Senses     []entry.Sense
	Syllables  []vnphon.Syllable // vi; nil when the headword has no rhyme keys
	Segs       []en.Seg          // en; nil when the headword has no pronunciation
	SegWord    []int             // en: word index of each segment, for highlighting (nil if unknown)
	Confidence float64           // phonetic confidence
	RhymeError bool
}

// Snapshot is one immutable generation of the index; share it freely.
type Snapshot struct {
	Gen      uint64
	BuiltAt  time.Time
	ConfigID string

	entries []Entry         // every approved entry with a definition
	byID    map[int64]int32 // entry id -> position in entries
	langs   map[entry.Lang]*langIndex
	enPron  *en.Pronouncer // pronounces English queries
}

type langIndex struct {
	cands []rhyme.Candidate // the rhymable entries
	byKey *csr.Table        // hash("dialect|level|n|key") -> positions in cands
	dict  *lookup.Index
	marks sync.Pool // *marks sized to cands
}

func indexKey(d rhyme.Dialect, l string, n int, key string) string {
	return string(d) + "|" + l + "|" + strconv.Itoa(n) + "|" + key
}

// buildKeys indexes every window key of every candidate, one builder per
// CPU, merged into one CSR table.
func buildKeys(cands []rhyme.Candidate, e *rhyme.Engine) *csr.Table {
	seed := csr.NewSeed()
	workers := max(1, min(runtime.GOMAXPROCS(0), len(cands)/1000+1))
	builders := make([]*csr.Builder, workers)
	var wg sync.WaitGroup
	for w := range workers {
		builders[w] = csr.NewBuilder(seed)
		wg.Add(1)
		go func(b *csr.Builder) {
			defer wg.Done()
			for pos := w; pos < len(cands); pos += workers {
				c := &cands[pos]
				keys := rhyme.WindowKeysEN(c.Segs)
				if c.Segs == nil {
					keys = e.WindowKeys(c.Syllables)
				}
				for _, k := range keys {
					b.Add(indexKey(k.Dialect, k.Level.String(), k.N, k.Key), int32(pos))
				}
			}
		}(builders[w])
	}
	wg.Wait()
	builders[0].Merge(builders[1:]...)
	return builders[0].Build()
}

// Build makes a snapshot; it refuses data whose stored rhyme keys came from
// a different rhyme config than e (ADR-003 D1). The key index itself is
// computed here from syllables and segments, in parallel — the same
// deterministic keys as rhyme_keys, without reading millions of rows
// (ADR-002 §5).
func Build(data *ports.IndexData, e *rhyme.Engine) (*Snapshot, error) {
	if data.ConfigID != e.ConfigID() {
		return nil, fmt.Errorf("%w (stored %.12q, running %.12q): run playword-cli rebuild-keys",
			ports.ErrKeysConfigMismatch, data.ConfigID, e.ConfigID())
	}
	s := &Snapshot{
		BuiltAt:  time.Now(),
		ConfigID: data.ConfigID,
		byID:     make(map[int64]int32, len(data.Entries)),
		langs:    map[entry.Lang]*langIndex{},
		enPron:   en.NewPronouncer(data.Pronunciations),
	}
	dicts := map[entry.Lang][]lookup.Entry{}
	for _, g := range mergeSources(data) {
		ie, senses, alias := g.primary, g.senses, g.aliases
		if len(senses) == 0 {
			continue // not explainable (rule 2)
		}
		lang := ie.Lang
		if lang == "" {
			lang = entry.VI
		}
		li := s.langs[lang]
		if li == nil {
			li = &langIndex{}
			s.langs[lang] = li
		}
		syl, segs := data.Syllables[ie.ID], data.Segments[ie.ID]
		rhymable := len(syl) > 0 || len(segs) > 0
		for _, id := range alias {
			s.byID[id] = int32(len(s.entries))
		}
		s.entries = append(s.entries, Entry{ID: ie.ID, Lang: lang, Headword: ie.Headword, POS: ie.POS, Source: ie.Source,
			IsSlang: ie.IsSlang, Trend: ie.Trend, Tags: ie.Tags, Senses: senses, Syllables: syl, Segs: segs,
			Confidence: ie.Confidence, RhymeError: !rhymable})
		dicts[lang] = append(dicts[lang], lookup.Entry{ID: ie.ID, Word: ie.Headword, Freq: ie.Zipf, Senses: senses})
		if !rhymable {
			continue
		}
		c := rhyme.Candidate{ID: ie.ID, Text: ie.Headword, Zipf: ie.Zipf, Trend: ie.Trend, IsSlang: ie.IsSlang,
			IsProper: slices.Contains(ie.Tags, "proper_noun")}
		if lang == entry.EN {
			c.Segs = segs
			s.entries[len(s.entries)-1].SegWord = s.segWords(ie.Headword, len(segs))
		} else {
			c.Syllables, c.Codes = syl, e.Codes(syl)
		}
		li.cands = append(li.cands, c)
	}
	for lang, li := range s.langs {
		li.dict = lookup.NewIndex(dicts[lang])
		li.byKey = buildKeys(li.cands, e)
		n := len(li.cands)
		li.marks.New = func() any { return &marks{seen: make([]uint32, n)} }
	}
	return s, nil
}

// segWords maps each rhyme segment of an English headword to the word it
// comes from, by pronouncing the words one by one; nil when that does not
// add up (the stored pronunciation came from an older dictionary).
func (s *Snapshot) segWords(headword string, total int) []int {
	var out []int
	for i, w := range strings.FieldsFunc(headword, func(r rune) bool { return r == ' ' || r == '-' }) {
		phones, _, err := s.enPron.Pronounce(w)
		if err != nil {
			return nil
		}
		segs, err := en.Segments(phones)
		if err != nil && !errors.Is(err, en.ErrNoVowel) {
			return nil
		}
		for range segs {
			out = append(out, i)
		}
	}
	if len(out) != total {
		return nil
	}
	return out
}

// sourceRank orders sources when one word comes from several (ADR-003 D3):
// edited dictionaries first, then Wiktionary, then community slang.
var sourceRank = map[string]int{"hnd": 0, "oewn": 0, "wiktionary-vi": 1, "community": 2}

func rank(source string) int {
	if r, ok := sourceRank[source]; ok {
		return r
	}
	return 3
}

type merged struct {
	primary ports.IndexEntry
	source  string // the primary's own source (primary.Source lists them all)
	senses  []entry.Sense
	aliases []int64 // every source entry id of the word, primary included
}

// mergeSources folds entries of the same (lang, normalized) from different
// sources into one: the best-ranked source's entry, with the others' senses
// appended (duplicates dropped), sources listed, slang and trend combined.
func mergeSources(data *ports.IndexData) []merged {
	type key struct {
		lang entry.Lang
		norm string
	}
	idx := map[key]int{}
	var out []merged
	for _, ie := range data.Entries {
		if ie.Lang == "" {
			ie.Lang = entry.VI
		}
		norm := ie.Normalized
		if norm == "" {
			norm = entry.Normalize(ie.Headword)
		}
		k := key{ie.Lang, norm}
		i, ok := idx[k]
		if !ok {
			idx[k] = len(out)
			out = append(out, merged{primary: ie, source: ie.Source, senses: data.Senses[ie.ID], aliases: []int64{ie.ID}})
			continue
		}
		m := &out[i]
		m.aliases = append(m.aliases, ie.ID)
		other := ie
		if rank(ie.Source) < rank(m.source) && len(data.Senses[ie.ID]) > 0 {
			sources := m.primary.Source
			other, m.primary, m.source = m.primary, ie, ie.Source
			other.Source = sources
			m.senses = append(slices.Clone(data.Senses[ie.ID]), m.senses...)
		} else {
			m.senses = append(m.senses, data.Senses[other.ID]...)
		}
		m.senses = dedupeSenses(m.senses)
		p := &m.primary
		for _, src := range strings.Split(other.Source, ", ") {
			p.Source = joinSources(p.Source, src)
		}
		p.IsSlang = p.IsSlang || other.IsSlang
		p.Trend = max(p.Trend, other.Trend)
		p.Zipf = max(p.Zipf, other.Zipf)
		for _, t := range other.Tags {
			if !slices.Contains(p.Tags, t) {
				p.Tags = append(slices.Clip(p.Tags), t)
			}
		}
		if len(data.Syllables[p.ID]) == 0 && len(data.Segments[p.ID]) == 0 {
			// the primary has no pronunciation; borrow the other's
			if syl := data.Syllables[other.ID]; len(syl) > 0 {
				data.Syllables[p.ID] = syl
			}
			if segs := data.Segments[other.ID]; len(segs) > 0 {
				data.Segments[p.ID] = segs
			}
		}
	}
	return out
}

func dedupeSenses(in []entry.Sense) []entry.Sense {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		k := entry.Normalize(strings.TrimRight(s.Gloss, ". "))
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

func joinSources(a, b string) string {
	for _, s := range strings.Split(a, ", ") {
		if s == b {
			return a
		}
	}
	return a + ", " + b
}

// Len is the number of entries, all languages.
func (s *Snapshot) Len() int { return len(s.entries) }

// Rhymable is the number of entries with rhyme keys, all languages.
func (s *Snapshot) Rhymable() int {
	n := 0
	for _, li := range s.langs {
		n += len(li.cands)
	}
	return n
}

// Langs lists the languages the snapshot holds.
func (s *Snapshot) Langs() []entry.Lang {
	var out []entry.Lang
	for l := range s.langs {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// Dictionary is the lookup index over every entry of lang: exact lookup,
// autocomplete with diacritic-free/telex/typo tolerance, reverse search.
// It is nil when the language has no entries.
func (s *Snapshot) Dictionary(lang entry.Lang) *lookup.Index {
	if li := s.langs[lang]; li != nil {
		return li.dict
	}
	return nil
}

// PronounceEN gives the rhyme segments of an English query and the
// confidence of its pronunciation.
func (s *Snapshot) PronounceEN(text string) ([]en.Seg, float64, error) {
	phones, conf, err := s.enPron.Pronounce(text)
	if err != nil {
		return nil, 0, err
	}
	segs, err := en.Segments(phones)
	return segs, conf, err
}

// Entry returns the display record of an entry.
func (s *Snapshot) Entry(id int64) *Entry {
	if i, ok := s.byID[id]; ok {
		return &s.entries[i]
	}
	return nil
}

// Search gathers candidates through the key index — every level up to
// q.Level and every suffix length — then ranks them with the domain's
// reference SearchIn, so the index can only be faster, never different.
func (s *Snapshot) Search(e *rhyme.Engine, q rhyme.Query) (rhyme.Response, error) {
	lang := q.Lang
	if lang == "" {
		lang = entry.VI
	}
	li := s.langs[lang]
	if li == nil {
		return e.SearchIn(q, nil, []int32{}) // validates q, finds nothing
	}
	type keyer func(l rhyme.Level, n int) string
	var length int
	var key keyer
	var levels []rhyme.Level
	if lang == entry.EN {
		used := q.Segs[max(0, len(q.Segs)-rhyme.MaxSyl):]
		length, levels = len(used), rhyme.ENLevels
		key = func(l rhyme.Level, n int) string { return rhyme.PhraseKeyEN(l, used[len(used)-n:]) }
	} else {
		used := q.Syllables[max(0, len(q.Syllables)-rhyme.MaxSyl):]
		qm := e.Map(q.Dialect, used)
		length, levels = len(qm), rhyme.Levels
		key = func(l rhyme.Level, n int) string { return e.Keyer().PhraseKey(l, qm[len(qm)-n:]) }
	}

	dialect := q.Dialect
	if lang == entry.EN {
		dialect = "" // English keys have no dialect
	}
	m := li.marks.Get().(*marks)
	defer li.marks.Put(m)
	m.next()
	subset := []int32{}
	for n := min(length, q.MaxSyl); n >= max(q.MinSyl, 1); n-- {
		for _, l := range levels {
			if l > q.Level {
				break
			}
			for _, pos := range li.byKey.Get(indexKey(dialect, l.String(), n, key(l, n))) {
				if m.seen[pos] != m.stamp {
					m.seen[pos] = m.stamp
					subset = append(subset, pos)
				}
			}
		}
	}
	return e.SearchIn(q, li.cands, subset)
}

// marks deduplicates candidate positions without clearing between queries:
// a position is seen when its mark equals the current stamp.
type marks struct {
	stamp uint32
	seen  []uint32
}

func (m *marks) next() {
	if m.stamp++; m.stamp == 0 { // wrapped: old marks could collide
		clear(m.seen)
		m.stamp = 1
	}
}

// Holder publishes the current snapshot; readers Load() without locking.
type Holder struct {
	cur atomic.Pointer[Snapshot]
	gen atomic.Uint64
}

// Load returns the current snapshot, or nil before the first build.
func (h *Holder) Load() *Snapshot { return h.cur.Load() }

// Rebuild loads data and swaps in a new snapshot; on any error the old one
// keeps serving.
func (h *Holder) Rebuild(ctx context.Context, loader ports.IndexLoader, e *rhyme.Engine) (*Snapshot, error) {
	data, err := loader.LoadIndex(ctx)
	if err != nil {
		return nil, err
	}
	s, err := Build(data, e)
	if err != nil {
		return nil, err
	}
	s.Gen = h.gen.Add(1)
	h.cur.Store(s)
	return s, nil
}

// SourceCounts is how many entries each source contributed; an entry merged
// from several sources ("hnd, wiktionary-vi") counts for each of them.
func (s *Snapshot) SourceCounts() map[string]int {
	out := map[string]int{}
	for i := range s.entries {
		for _, src := range strings.Split(s.entries[i].Source, ",") {
			if src = strings.TrimSpace(src); src != "" {
				out[src]++
			}
		}
	}
	return out
}
