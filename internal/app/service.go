// Package app holds the use cases the HTTP layer calls: SearchRhymes,
// LookupWord, Suggest and Parse (spec §5).
package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

	"playword/internal/adapters/index/lookup"
	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/pkg/vnphon"
)

// Errors the HTTP layer maps to status codes.
var (
	ErrNotReady        = errors.New("index not loaded yet")
	ErrNotFound        = errors.New("not found")
	ErrUnsupportedLang = errors.New("language not supported yet")
	ErrBadCursor       = errors.New("invalid cursor")
	ErrNoPronunciation = errors.New("no pronunciation")
	ErrInvalidParam    = errors.New("invalid parameter")
)

// Paging limits.
const (
	DefaultLimit = 50
	MaxLimit     = 100
	// MaxDepth bounds how far a result list can be paged; ranked lists of
	// this length are what the cache holds (ADR-002 §4).
	MaxDepth = 500
)

// IndexSource returns the current snapshot, or nil before the first build.
type IndexSource interface {
	Load() *rhymeindex.Snapshot
}

// Service implements the use cases over the current index snapshot.
type Service struct {
	index  IndexSource
	engine *rhyme.Engine
	cache  *lru.Cache[string, *ranked]
	gen    atomic.Uint64 // snapshot generation the cache belongs to
	flight singleflight.Group

	cacheHits, cacheMisses atomic.Uint64
}

// CacheStats reports result-cache hits and misses (for /metrics).
func (s *Service) CacheStats() (hits, misses uint64) { return s.cacheHits.Load(), s.cacheMisses.Load() }

// NewService keeps up to cacheSize ranked result lists.
func NewService(index IndexSource, engine *rhyme.Engine, cacheSize int) *Service {
	c, _ := lru.New[string, *ranked](max(cacheSize, 1))
	return &Service{index: index, engine: engine, cache: c}
}

// Engine is the rhyme engine queries are keyed with.
func (s *Service) Engine() *rhyme.Engine { return s.engine }

// Snapshot returns the current index or ErrNotReady.
func (s *Service) Snapshot() (*rhymeindex.Snapshot, error) {
	snap := s.index.Load()
	if snap == nil {
		return nil, ErrNotReady
	}
	return snap, nil
}

// RhymeParams are the inputs of SearchRhymes (spec §6 query parameters).
type RhymeParams struct {
	Text          string
	Lang          entry.Lang
	Dialect       rhyme.Dialect
	Level         rhyme.Level
	MinSyl        int
	MaxSyl        int
	Match         rhyme.Align
	IncludeSlang  bool
	IncludeProper bool
	Limit         int
	Cursor        string
	// ExtraOver, when set, keeps only results that do not already match in
	// that dialect ("khớp thêm ở giọng Nam", spec §1.8).
	ExtraOver rhyme.Dialect
}

// DefaultRhymeParams mirrors the spec defaults.
func DefaultRhymeParams(text string) RhymeParams {
	return RhymeParams{Text: text, Lang: entry.VI, Dialect: rhyme.Bac, Level: rhyme.L25, MinSyl: 1,
		MaxSyl: rhyme.MaxSyl, Match: rhyme.AlignSuffix, IncludeSlang: true, IncludeProper: true, Limit: DefaultLimit}
}

// Hit is one ranked rhyme with the entry it points to.
type Hit struct {
	Entry     *rhymeindex.Entry
	Matched   int
	Level     rhyme.Level
	Start     int
	End       int
	Score     float64
	Identical int // matched syllables that repeat the query's own word
	Distance  int // phonetic distance inside the level (rhyme/closeness.go)
}

// RhymeResult is one page of hits.
type RhymeResult struct {
	Params       RhymeParams
	Query        []vnphon.Syllable // vi
	QuerySegs    []en.Seg          // en
	Confidence   float64           // en: pronunciation confidence of the query
	Hits         []Hit
	Offset       int
	PrevMatched  int  // matched syllables of the hit before this page (0 on the first page)
	Prev         *Hit // the hit before this page, nil on the first page
	Total        int  // ranked results in all (at most MaxDepth)
	NextCursor   string
	Warnings     []string
	IndexChanged bool // the cursor came from an older snapshot
}

type ranked struct {
	query    []vnphon.Syllable
	segs     []en.Seg
	conf     float64
	hits     []Hit
	warnings []string
}

// SearchRhymes ranks rhymes for p and returns the page its cursor points at.
func (s *Service) SearchRhymes(ctx context.Context, p RhymeParams) (*RhymeResult, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	if snap.Dictionary(p.Lang) == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedLang, p.Lang)
	}
	p.Limit = min(max(p.Limit, 1), MaxLimit)
	offset, gen, err := decodeCursor(p.Cursor)
	if err != nil {
		return nil, err
	}

	list, err := s.ranked(ctx, snap, p)
	if err != nil {
		return nil, err
	}
	hits := list.hits
	if p.ExtraOver != "" && p.ExtraOver != p.Dialect {
		base := p
		base.Dialect, base.ExtraOver = p.ExtraOver, ""
		baseList, err := s.ranked(ctx, snap, base)
		if err != nil {
			return nil, err
		}
		have := make(map[int64]bool, len(baseList.hits))
		for _, h := range baseList.hits {
			have[h.Entry.ID] = true
		}
		hits = make([]Hit, 0, len(list.hits))
		for _, h := range list.hits {
			if !have[h.Entry.ID] {
				hits = append(hits, h)
			}
		}
	}

	res := &RhymeResult{Params: p, Query: list.query, QuerySegs: list.segs, Confidence: list.conf, Warnings: list.warnings, Offset: offset,
		IndexChanged: p.Cursor != "" && gen != snap.Gen}
	res.Total = len(hits)
	if offset > 0 && offset <= len(hits) {
		res.PrevMatched, res.Prev = hits[offset-1].Matched, &hits[offset-1]
	}
	if offset < len(hits) {
		end := min(offset+p.Limit, len(hits))
		res.Hits = hits[offset:end]
		if end < len(hits) {
			res.NextCursor = encodeCursor(snap.Gen, end)
		}
	}
	return res, ctx.Err()
}

// Explanation is why one result rhymes: the hit as ranked for the search,
// and each matched syllable next to the query's.
type Explanation struct {
	Hit   Hit
	Pairs []rhyme.Pair
}

// Explain finds entry id in the ranking for p and says why it rhymes. It
// reads the same (cached) ranking as SearchRhymes, so the panel agrees with
// the list. It returns nil when the entry is not among the results, and for
// English, which has no per-syllable explanation yet.
func (s *Service) Explain(ctx context.Context, p RhymeParams, id int64) (*Explanation, error) {
	if p.Lang == entry.EN {
		return nil, nil
	}
	snap, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	list, err := s.ranked(ctx, snap, p)
	if err != nil {
		return nil, err
	}
	for _, h := range list.hits {
		if h.Entry != nil && h.Entry.ID == id {
			pairs := s.engine.Explain(list.query, p.Dialect, h.Entry.Syllables, h.Start, h.End)
			if pairs == nil {
				return nil, nil
			}
			return &Explanation{Hit: h, Pairs: pairs}, nil
		}
	}
	return nil, nil
}

// ranked returns the full ranked list for p (ignoring paging), from the
// cache or computed once for all concurrent callers.
func (s *Service) ranked(ctx context.Context, snap *rhymeindex.Snapshot, p RhymeParams) (*ranked, error) {
	if old := s.gen.Load(); old != snap.Gen && s.gen.CompareAndSwap(old, snap.Gen) {
		s.cache.Purge() // results of an older snapshot must not be served
	}
	q := rhyme.Query{Lang: p.Lang, Dialect: p.Dialect, Level: p.Level, MinSyl: p.MinSyl, MaxSyl: p.MaxSyl,
		Match: p.Match, ExcludeSlang: !p.IncludeSlang, ExcludeProper: !p.IncludeProper, Limit: MaxDepth}
	var norm string
	conf := 1.0
	if p.Lang == entry.EN {
		norm = entry.Normalize(p.Text)
		segs, c, err := snap.PronounceEN(norm)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNoPronunciation, err)
		}
		q.Segs, q.Text, q.Dialect, conf = segs, norm, "", c
	} else {
		syl, err := vnphon.ParsePhrase(p.Text)
		if err != nil {
			return nil, err
		}
		q.Syllables = syl
		norm, _ = vnphon.RenderPhrase(syl)
	}
	key := fmt.Sprintf("%d|%s|%s|%s|%s|%d|%d|%s|%t|%t", snap.Gen, p.Lang, norm, q.Dialect, p.Level, p.MinSyl, p.MaxSyl, p.Match, p.IncludeSlang, p.IncludeProper)
	if r, ok := s.cache.Get(key); ok {
		s.cacheHits.Add(1)
		return r, nil
	}
	s.cacheMisses.Add(1)
	v, err, _ := s.flight.Do(key, func() (any, error) {
		resp, err := snap.Search(s.engine, q)
		if err != nil {
			return nil, err
		}
		r := &ranked{query: resp.Syllables, segs: q.Segs, conf: conf, warnings: resp.Warnings, hits: make([]Hit, 0, len(resp.Results))}
		for _, x := range resp.Results {
			r.hits = append(r.hits, Hit{Entry: snap.Entry(x.Candidate.ID), Matched: x.Matched, Level: x.Level,
				Start: x.Start, End: x.End, Score: x.Score, Identical: int(x.Identical), Distance: int(x.Distance)})
		}
		s.cache.Add(key, r)
		return r, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*ranked), ctx.Err()
}

func encodeCursor(gen uint64, offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(gen, 10) + ":" + strconv.Itoa(offset)))
}

func decodeCursor(c string) (offset int, gen uint64, err error) {
	if c == "" {
		return 0, 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, 0, ErrBadCursor
	}
	g, o, ok := strings.Cut(string(b), ":")
	gen, err1 := strconv.ParseUint(g, 10, 64)
	offset, err2 := strconv.Atoi(o)
	if !ok || err1 != nil || err2 != nil || offset < 0 || offset > MaxDepth {
		return 0, 0, ErrBadCursor
	}
	return offset, gen, nil
}

// LookupWord finds a headword exactly (case and Unicode form ignored).
func (s *Service) LookupWord(lang entry.Lang, text string) (*rhymeindex.Entry, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	dict := snap.Dictionary(lang)
	if dict == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	if e := dict.Lookup(text); e != nil {
		return snap.Entry(e.ID), nil
	}
	return nil, ErrNotFound
}

// Suggest completes a partly typed headword (diacritic-free, telex, typos).
func (s *Service) Suggest(lang entry.Lang, q string, limit int) ([]lookup.Suggestion, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	dict := snap.Dictionary(lang)
	if dict == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	return dict.Suggest(q, limit), nil
}

// SearchMeanings is the reverse lookup: headwords whose definitions contain q.
func (s *Service) SearchMeanings(lang entry.Lang, q string, limit int) ([]*rhymeindex.Entry, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	dict := snap.Dictionary(lang)
	if dict == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	var out []*rhymeindex.Entry
	for _, e := range dict.SearchMeanings(q, limit) {
		out = append(out, snap.Entry(e.ID))
	}
	return out, nil
}

// ParseEN pronounces English text (debug endpoint, spec §6).
func (s *Service) ParseEN(text string) ([]en.Seg, float64, map[rhyme.Level]string, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return nil, 0, nil, err
	}
	segs, conf, err := snap.PronounceEN(text)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%w: %v", ErrNoPronunciation, err)
	}
	keys := map[rhyme.Level]string{}
	for _, l := range rhyme.ENLevels {
		keys[l] = rhyme.PhraseKeyEN(l, segs)
	}
	return segs, conf, keys, nil
}

// Parse splits text into syllables (debug endpoint, spec §6).
func (s *Service) Parse(lang entry.Lang, text string) ([]vnphon.Syllable, map[rhyme.Dialect]map[rhyme.Level]string, error) {
	if lang != entry.VI {
		return nil, nil, fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}
	syl, err := vnphon.ParsePhrase(text)
	if err != nil {
		return nil, nil, err
	}
	keys := map[rhyme.Dialect]map[rhyme.Level]string{}
	for _, d := range rhyme.Dialects {
		keys[d] = s.engine.Keys(d, syl)
	}
	return syl, keys, nil
}

// EntryByID returns an entry of the current snapshot.
func (s *Service) EntryByID(id int64) (*rhymeindex.Entry, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	if e := snap.Entry(id); e != nil {
		return e, nil
	}
	return nil, ErrNotFound
}

// MaxCheckText bounds a rhyme check, in characters (a book's lyrics).
const MaxCheckText = 20000

// RhymeCheck colours the multi-syllable rhymes of a text (the writing
// desk's "Kiểm tra vần"). Only Vietnamese levels apply; English words break
// chains for now.
func (s *Service) RhymeCheck(text string, d rhyme.Dialect, l rhyme.Level) (rhyme.CheckResult, error) {
	if utf8.RuneCountInString(text) > MaxCheckText {
		return rhyme.CheckResult{}, fmt.Errorf("%w: text longer than %d characters", ErrInvalidParam, MaxCheckText)
	}
	if !utf8.ValidString(text) {
		return rhyme.CheckResult{}, fmt.Errorf("%w: text is not UTF-8", ErrInvalidParam)
	}
	if l.IsEnglish() {
		return rhyme.CheckResult{}, fmt.Errorf("%w: rhyme check takes a Vietnamese level (L0…L3)", ErrInvalidParam)
	}
	return s.engine.Check(text, rhyme.CheckOptions{Dialect: d, Level: l}), nil
}
