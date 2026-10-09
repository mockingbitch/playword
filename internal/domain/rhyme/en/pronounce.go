package en

import (
	"fmt"
	"strings"
	"unicode"
)

// DerivedConfidence is the phonetic_confidence of a pronunciation built
// from a dictionary stem plus a suffix rule (spec §1.6).
const DerivedConfidence = 0.7

// Pronouncer looks words up in a pronunciation dictionary (CMUdict) and
// derives inflected forms it lacks from their stems.
type Pronouncer struct {
	dict map[string][]string // lowercase word -> phones
}

// NewPronouncer wraps dict (lowercase word -> phones).
func NewPronouncer(dict map[string][]string) *Pronouncer { return &Pronouncer{dict: dict} }

// Pronounce returns the phones of a phrase and their confidence: 1 when
// every word is in the dictionary, DerivedConfidence when a suffix rule was
// needed. Words are split on spaces and hyphens.
func (p *Pronouncer) Pronounce(phrase string) ([]string, float64, error) {
	words := strings.FieldsFunc(strings.ToLower(phrase), func(r rune) bool { return unicode.IsSpace(r) || r == '-' })
	if len(words) == 0 {
		return nil, 0, fmt.Errorf("empty phrase")
	}
	conf := 1.0
	var out []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"()")
		ph, ok := p.dict[w]
		if !ok {
			if ph, ok = p.derive(w); !ok {
				return nil, 0, fmt.Errorf("no pronunciation for %q", w)
			}
			conf = DerivedConfidence
		}
		out = append(out, ph...)
	}
	return out, conf, nil
}

// derive tries suffix rules: plural/possessive -s, past -ed, -ing and a few
// derivational endings, with the usual spelling changes of the stem.
func (p *Pronouncer) derive(w string) ([]string, bool) {
	stem := func(cands ...string) ([]string, bool) {
		for _, c := range cands {
			if ph, ok := p.dict[c]; ok && c != "" {
				return ph, true
			}
		}
		return nil, false
	}
	undouble := func(s string) string {
		if n := len(s); n >= 2 && s[n-1] == s[n-2] {
			return s[:n-1]
		}
		return s
	}
	switch {
	case strings.HasSuffix(w, "'s"):
		if ph, ok := stem(strings.TrimSuffix(w, "'s")); ok {
			return withS(ph), true
		}
	case strings.HasSuffix(w, "ies"):
		b := strings.TrimSuffix(w, "ies") // flies, monies, pies
		if ph, ok := stem(b+"y", b+"ey", b+"ie"); ok {
			return withS(ph), true
		}
	case strings.HasSuffix(w, "ing"):
		b := strings.TrimSuffix(w, "ing")
		if ph, ok := stem(b, b+"e", undouble(b)); ok {
			return append(clone(ph), "IH0", "NG"), true
		}
	case strings.HasSuffix(w, "ed"):
		b := strings.TrimSuffix(w, "ed")
		if ph, ok := stem(b, b+"e", undouble(b)); ok {
			return withEd(ph), true
		}
	case strings.HasSuffix(w, "ness"):
		if ph, ok := stem(strings.TrimSuffix(w, "ness")); ok {
			return append(clone(ph), "N", "AH0", "S"), true
		}
	case strings.HasSuffix(w, "ly"):
		if ph, ok := stem(strings.TrimSuffix(w, "ly")); ok {
			return append(clone(ph), "L", "IY0"), true
		}
	case strings.HasSuffix(w, "est"):
		b := strings.TrimSuffix(w, "est")
		if ph, ok := stem(b, b+"e", undouble(b)); ok {
			return append(clone(ph), "AH0", "S", "T"), true
		}
	case strings.HasSuffix(w, "er"):
		b := strings.TrimSuffix(w, "er")
		if ph, ok := stem(b, b+"e", undouble(b)); ok {
			return append(clone(ph), "ER0"), true
		}
	case strings.HasSuffix(w, "es"):
		if ph, ok := stem(strings.TrimSuffix(w, "es"), strings.TrimSuffix(w, "s")); ok {
			return withS(ph), true
		}
	case strings.HasSuffix(w, "s"):
		if ph, ok := stem(strings.TrimSuffix(w, "s")); ok {
			return withS(ph), true
		}
	}
	return nil, false
}

var (
	sibilants = map[string]bool{"S": true, "Z": true, "SH": true, "ZH": true, "CH": true, "JH": true}
	voiceless = map[string]bool{"P": true, "T": true, "K": true, "F": true, "TH": true}
)

func clone(ph []string) []string { return append([]string(nil), ph...) }

func last(ph []string) string { return ph[len(ph)-1] }

// withS adds the -s ending: /ɪz/ after sibilants, /s/ after voiceless, /z/ otherwise.
func withS(ph []string) []string {
	switch l := last(ph); {
	case sibilants[l]:
		return append(clone(ph), "IH0", "Z")
	case voiceless[l]:
		return append(clone(ph), "S")
	}
	return append(clone(ph), "Z")
}

// withEd adds the -ed ending: /ɪd/ after t or d, /t/ after voiceless, /d/ otherwise.
func withEd(ph []string) []string {
	switch l := last(ph); {
	case l == "T" || l == "D":
		return append(clone(ph), "IH0", "D")
	case voiceless[l] || l == "S" || l == "SH" || l == "CH":
		return append(clone(ph), "T")
	}
	return append(clone(ph), "D")
}
