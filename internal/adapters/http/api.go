package httpadapter

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/app"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/pkg/vnphon"
)

type syllableJSON struct {
	Onset   string `json:"onset"`
	Medial  string `json:"medial,omitempty"`
	Nucleus string `json:"nucleus"`
	Coda    string `json:"coda"`
	Tone    int    `json:"tone"`
}

func syllablesJSON(p []vnphon.Syllable) []syllableJSON {
	out := make([]syllableJSON, len(p))
	for i, s := range p {
		out[i] = syllableJSON{s.Onset, s.Medial, s.Nucleus, s.Coda, s.Tone}
	}
	return out
}

// entryPhonetic is phonetic() for Vietnamese entries and the ARPAbet
// segments ("AH1 N") for English ones.
func entryPhonetic(e *rhymeindex.Entry) []string {
	if e.Lang == entry.EN {
		return segPhonetic(e.Segs)
	}
	return phonetic(e.Syllables)
}

func segPhonetic(segs []en.Seg) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = s.String()
	}
	return out
}

// phonetic renders "m-AW-j-1" per syllable (spec §6), skipping empty parts.
func phonetic(p []vnphon.Syllable) []string {
	out := make([]string, len(p))
	for i, s := range p {
		var parts []string
		for _, x := range []string{s.Onset, s.Medial, s.Nucleus, s.Coda} {
			if x != "" {
				parts = append(parts, x)
			}
		}
		out[i] = strings.Join(append(parts, strconv.Itoa(s.Tone)), "-")
	}
	return out
}

type definitionJSON struct {
	POS      string   `json:"pos,omitempty"`
	Gloss    string   `json:"gloss"`
	Examples []string `json:"examples,omitempty"`
}

func definitions(e *rhymeindex.Entry) []definitionJSON {
	out := make([]definitionJSON, len(e.Senses))
	for i, s := range e.Senses {
		out[i] = definitionJSON{s.POS, s.Gloss, s.Examples}
	}
	return out
}

type alignmentJSON struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type rhymeHitJSON struct {
	ID          int64            `json:"id"`
	Text        string           `json:"text"`
	Lang        entry.Lang       `json:"lang"`
	Matched     int              `json:"matched_syllables"`
	Level       rhyme.Level      `json:"level"`
	Distance    int              `json:"distance"`      // phonetic distance inside the level; 0 = perfect echo
	Repeats     int              `json:"repeats_query"` // matched syllables that are the query's own word
	Score       float64          `json:"score"`         // frequency/trend/length tie-breaker; the order is by tiers (ADR-004)
	Alignment   alignmentJSON    `json:"alignment"`
	POS         string           `json:"pos,omitempty"`
	Definitions []definitionJSON `json:"definitions"`
	IsSlang     bool             `json:"is_slang"`
	IsProper    bool             `json:"is_proper"`
	TrendScore  float64          `json:"trend_score"`
	Source      string           `json:"source"`
	Phonetic    []string         `json:"phonetic"`
	Confidence  float64          `json:"phonetic_confidence"`
}

func (s *Server) apiRhymes(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	u := currentUser(r)
	p, err := rhymeParamsFor(r.URL.Query(), u)
	if err == nil && p.Text == "" {
		err = rhymeQueryErr("q is required")
	}
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	res, err := s.svc.SearchRhymes(r.Context(), p)
	s.recordSearch(r, p, res, start)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	hits := make([]rhymeHitJSON, len(res.Hits))
	for i, h := range res.Hits {
		hits[i] = rhymeHitJSON{
			ID: h.Entry.ID, Text: h.Entry.Headword, Lang: p.Lang, Matched: h.Matched, Level: h.Level,
			Distance: h.Distance, Repeats: h.Identical,
			Score: round1(h.Score), Alignment: alignmentJSON{h.Start, h.End}, POS: h.Entry.POS,
			Definitions: definitions(h.Entry), IsSlang: h.Entry.IsSlang, IsProper: isProper(h.Entry),
			TrendScore: h.Entry.Trend, Source: h.Entry.Source, Phonetic: entryPhonetic(h.Entry), Confidence: h.Entry.Confidence,
		}
	}
	if u != nil {
		w.Header().Set("Cache-Control", "private, max-age=60") // shaped by the user's defaults
	} else {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":         queryJSON(p, res),
		"results":       hits,
		"next_cursor":   res.NextCursor,
		"warnings":      nonNil(res.Warnings),
		"index_changed": res.IndexChanged,
	})
}

func (s *Server) apiWord(w http.ResponseWriter, r *http.Request) {
	e, err := s.svc.LookupWord(entry.Lang(r.PathValue("lang")), r.PathValue("text"))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"id": e.ID, "text": e.Headword, "lang": r.PathValue("lang"), "pos": e.POS,
		"definitions": definitions(e), "is_slang": e.IsSlang, "is_proper": isProper(e), "tags": nonNil(e.Tags),
		"source": e.Source, "rhymable": !e.RhymeError, "phonetic": entryPhonetic(e), "phonetic_confidence": e.Confidence,
	})
}

func queryJSON(p app.RhymeParams, res *app.RhymeResult) map[string]any {
	q := map[string]any{"text": p.Text, "lang": p.Lang, "level": p.Level}
	if p.Lang == entry.EN {
		q["phonetic"], q["phonetic_confidence"] = segPhonetic(res.QuerySegs), res.Confidence
	} else {
		q["dialect"], q["syllables"] = p.Dialect, syllablesJSON(res.Query)
	}
	return q
}

func (s *Server) apiParse(w http.ResponseWriter, r *http.Request) {
	lang := entry.Lang(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = entry.VI
	}
	if lang == entry.EN {
		segs, conf, keys, err := s.svc.ParseEN(r.URL.Query().Get("q"))
		if err != nil {
			writeProblem(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"text": r.URL.Query().Get("q"), "lang": lang,
			"phonetic": segPhonetic(segs), "phonetic_confidence": conf, "keys": keys})
		return
	}
	syl, keys, err := s.svc.Parse(lang, r.URL.Query().Get("q"))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": r.URL.Query().Get("q"), "lang": lang,
		"syllables": syllablesJSON(syl), "phonetic": phonetic(syl), "keys": keys})
}

func (s *Server) apiSuggest(w http.ResponseWriter, r *http.Request) {
	sg, err := s.svc.Suggest(langParam(r), r.URL.Query().Get("q"), 10)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	type row struct {
		Text  string `json:"text"`
		Gloss string `json:"gloss"`
	}
	out := make([]row, len(sg))
	for i, x := range sg {
		out[i] = row{x.Entry.Word, x.Entry.Gloss()}
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, out)
}

func isProper(e *rhymeindex.Entry) bool {
	for _, t := range e.Tags {
		if t == "proper_noun" {
			return true
		}
	}
	return false
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func rhymeQueryErr(msg string) error { return &queryError{msg} }

type queryError struct{ msg string }

func (e *queryError) Error() string { return e.msg }
func (e *queryError) Unwrap() error { return rhyme.ErrInvalidQuery }

// langParam is ?lang=, Vietnamese by default.
func langParam(r *http.Request) entry.Lang {
	if l := r.URL.Query().Get("lang"); l != "" {
		return entry.Lang(l)
	}
	return entry.VI
}
