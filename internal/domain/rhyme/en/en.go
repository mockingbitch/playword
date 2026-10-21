// Package en is English phonetics for rhyming (spec §1.6): ARPAbet
// pronunciations (CMUdict) split into rhyme segments, and the three
// strictness levels perfect ⊂ family ⊂ assonance.
package en

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoVowel is returned for a pronunciation without any vowel.
var ErrNoVowel = errors.New("pronunciation has no vowel")

// Seg is one rhyme segment: a vowel and the consonants after it, up to the
// next vowel. A phrase's rhyme from its k-th last vowel is its last k
// segments; the consonants before the first vowel never rhyme.
type Seg struct {
	Vowel  string   // ARPAbet vowel without stress: AH, IY, …
	Stress int      // 0 none, 1 primary, 2 secondary
	Tail   []string // consonants following the vowel
}

// Phones renders a segment back as ARPAbet phones with stress, the form
// stored in the syllables table.
func (s Seg) Phones() []string {
	return append([]string{s.Vowel + string(rune('0'+s.Stress))}, s.Tail...)
}

func (s Seg) String() string { return strings.Join(s.Phones(), " ") }

// ParsePhones splits a CMUdict pronunciation ("M AH1 N IY0") into phones.
func ParsePhones(pron string) []string { return strings.Fields(strings.ToUpper(pron)) }

// Segments splits phones into rhyme segments.
func Segments(phones []string) ([]Seg, error) {
	var out []Seg
	for _, p := range phones {
		if v, stress, ok := vowel(p); ok {
			out = append(out, Seg{Vowel: v, Stress: stress})
			continue
		}
		if !consonants[p] {
			return nil, fmt.Errorf("unknown ARPAbet phone %q", p)
		}
		if len(out) > 0 {
			out[len(out)-1].Tail = append(out[len(out)-1].Tail, p)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoVowel
	}
	return out, nil
}

// SegmentsFromRows rebuilds segments from stored phone rows (Seg.Phones).
func SegmentsFromRows(rows [][]string) ([]Seg, error) {
	out := make([]Seg, 0, len(rows))
	for _, r := range rows {
		s, err := Segments(r)
		if err != nil || len(s) != 1 {
			return nil, fmt.Errorf("bad stored segment %q", r)
		}
		out = append(out, s[0])
	}
	return out, nil
}

func vowel(p string) (string, int, bool) {
	if n := len(p); n >= 3 && p[n-1] >= '0' && p[n-1] <= '2' && vowelGroup[p[:n-1]] != "" {
		return p[:n-1], int(p[n-1] - '0'), true
	}
	if vowelGroup[p] != "" { // stress missing: treat as unstressed
		return p, 0, true
	}
	return "", 0, false
}

// Level is an English strictness level; lower is stricter and each level
// contains the previous one.
type Level int

// The English levels, strictest first.
const (
	Perfect   Level = iota // same vowels, stress and consonants from the matched vowel on
	Family                 // vowels and consonants from the same groups
	Assonance              // vowels from the same groups only
)

// Levels lists the levels from strictest to loosest.
var Levels = []Level{Perfect, Family, Assonance}

var levelNames = [...]string{Perfect: "perfect", Family: "family", Assonance: "assonance"}

func (l Level) String() string { return levelNames[l] }

// ParseLevel accepts perfect, family or assonance.
func ParseLevel(s string) (Level, bool) {
	for i, n := range levelNames {
		if strings.EqualFold(s, n) {
			return Level(i), true
		}
	}
	return 0, false
}

// Vowel groups (spec §1.6): AA/AO, IY/IH, EH/AE, UW/UH, AH/AX; the others
// stand alone.
var vowelGroup = map[string]string{
	"AA": "A", "AO": "A",
	"IY": "I", "IH": "I",
	"EH": "E", "AE": "E",
	"UW": "U", "UH": "U",
	"AH": "@", "AX": "@",
	"AW": "AW", "AY": "AY", "ER": "ER", "EY": "EY", "OW": "OW", "OY": "OY",
}

// Consonant groups (spec §1.6: nasal, voiced/unvoiced stops, fricatives);
// affricates join the fricatives and the approximants form their own group.
var consonantGroup = map[string]string{
	"M": "N", "N": "N", "NG": "N",
	"B": "B", "D": "B", "G": "B",
	"P": "P", "T": "P", "K": "P",
	"F": "F", "V": "F", "TH": "F", "DH": "F", "S": "F", "Z": "F", "SH": "F", "ZH": "F", "HH": "F", "CH": "F", "JH": "F",
	"L": "L", "R": "L", "W": "L", "Y": "L",
}

var consonants = func() map[string]bool {
	m := map[string]bool{}
	for c := range consonantGroup {
		m[c] = true
	}
	return m
}()

// Key is the key of one segment at level l; two segments rhyme at l iff
// their keys are equal.
func Key(l Level, s Seg) string {
	switch l {
	case Perfect:
		stress := ""
		if s.Stress > 0 {
			stress = "'"
		}
		return stress + s.Vowel + "." + strings.Join(s.Tail, "")
	case Family:
		var b strings.Builder
		b.WriteString(vowelGroup[s.Vowel])
		b.WriteByte('.')
		for _, c := range s.Tail {
			b.WriteString(consonantGroup[c])
		}
		return b.String()
	}
	return vowelGroup[s.Vowel]
}

// PhraseKey joins the keys of segs at level l.
func PhraseKey(l Level, segs []Seg) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = Key(l, s)
	}
	return strings.Join(parts, " ")
}

// SegLevel is the strictest level at which two segments rhyme. A perfect
// rhyme also needs the same stress (stressed or not): "money" (AH1 N IY0)
// rhymes perfectly with "honey" but only at family level with "company"
// (… AH0 N IY0).
func SegLevel(a, b Seg) (Level, bool) {
	if vowelGroup[a.Vowel] != vowelGroup[b.Vowel] {
		return 0, false
	}
	if a.Vowel == b.Vowel && (a.Stress > 0) == (b.Stress > 0) && equal(a.Tail, b.Tail, func(s string) string { return s }) {
		return Perfect, true
	}
	if equal(a.Tail, b.Tail, func(s string) string { return consonantGroup[s] }) {
		return Family, true
	}
	return Assonance, true
}

func equal(a, b []string, f func(string) string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if f(a[i]) != f(b[i]) {
			return false
		}
	}
	return true
}

// SameConsonantGroup reports whether two consonants share a group (nasals,
// voiced stops, unvoiced stops, fricatives, approximants).
func SameConsonantGroup(a, b string) bool {
	ga, ok := consonantGroup[a]
	return ok && ga == consonantGroup[b]
}
