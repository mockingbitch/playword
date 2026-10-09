// Package entry holds the dictionary record shared by ingest, storage and
// search.
package entry

import (
	"crypto/sha256"
	"encoding/json"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Lang is an ISO 639-1 language code.
type Lang string

// Supported languages.
const (
	VI Lang = "vi"
	EN Lang = "en"
)

// Sense is one meaning of a headword.
type Sense struct {
	POS      string   `json:"pos,omitempty"`
	Gloss    string   `json:"gloss"`
	Examples []string `json:"examples,omitempty"`
}

// Entry is a dictionary record as sources emit it.
type Entry struct {
	Headword string  `json:"headword"`
	Lang     Lang    `json:"lang"`
	POS      string  `json:"pos,omitempty"`
	Senses   []Sense `json:"senses"`
	IsSlang  bool    `json:"is_slang,omitempty"`
	// Respelling is how a loanword or slang is pronounced in the entry's
	// language ("chill" -> "chiu"); rhyme keys are computed from it (Q8).
	Respelling string   `json:"respelling,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	SourceRef  string   `json:"source_ref,omitempty"`
}

// Normalize is the natural-key form of a headword: NFC, lowercase, trimmed,
// single spaces.
func Normalize(s string) string {
	return strings.Join(strings.Fields(norm.NFC.String(strings.ToLower(s))), " ")
}

// HasDefinition reports whether e can be shown at all (rule 2: every result
// must be explainable).
func (e *Entry) HasDefinition() bool {
	for _, s := range e.Senses {
		if strings.TrimSpace(s.Gloss) != "" {
			return true
		}
	}
	return false
}

// ContentHash fingerprints everything a re-ingest could change, so an
// unchanged record is not rewritten (ADR-003 D6).
func (e *Entry) ContentHash() []byte {
	b, _ := json.Marshal(struct {
		H, P string
		S    []Sense
		T    []string
		Sl   bool
		R    string `json:",omitempty"`
	}{e.Headword, e.POS, e.Senses, e.Tags, e.IsSlang, e.Respelling})
	sum := sha256.Sum256(b)
	return sum[:]
}
