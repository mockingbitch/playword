// Package vnphon splits Vietnamese text into syllables following standard
// orthography: onset, medial, nucleus, coda and tone. It knows nothing about
// rhyme rules or dialects; see ADR-001.
package vnphon

import (
	"errors"
	"fmt"
)

// Nucleus codes (ADR-001 §2).
const (
	A   = "A"   // a
	AW  = "AW"  // ă, and the a of ay/au
	AA  = "AA"  // â
	E   = "E"   // e
	EE  = "EE"  // ê
	I   = "I"   // i/y
	O   = "O"   // o, oo
	OO  = "OO"  // ô
	OW  = "OW"  // ơ
	U   = "U"   // u
	UW  = "UW"  // ư
	IE  = "IE"  // ia/iê/ya/yê
	UO  = "UO"  // ua/uô
	UOW = "UOW" // ưa/ươ
)

// MedialW is the only medial: written o (hoa) or u (huy, qua).
const MedialW = "w"

// Nuclei and Codas list every valid code, in canonical order.
var (
	Nuclei = []string{A, AW, AA, E, EE, I, O, OO, OW, U, UW, IE, UO, UOW}
	Codas  = []string{"", "j", "w", "m", "n", "ng", "nh", "p", "t", "k", "ch"}
)

// Tones, numbered as in the spec.
const (
	ToneNgang = 1
	ToneHuyen = 2
	ToneSac   = 3
	ToneHoi   = 4
	ToneNga   = 5
	ToneNang  = 6
)

// Syllable is one parsed syllable; see ADR-001 §1 for the codes.
type Syllable struct {
	Onset   string `json:"onset"`
	Medial  string `json:"medial"`
	Nucleus string `json:"nucleus"`
	Coda    string `json:"coda"`
	Tone    int    `json:"tone"`
}

// IsStopCoda reports whether c is p, t, c (k) or ch; such syllables only
// carry sắc or nặng.
func IsStopCoda(c string) bool {
	return c == "p" || c == "t" || c == "k" || c == "ch"
}

// ErrInvalidSyllable is matched by every parse failure (errors.Is).
var ErrInvalidSyllable = errors.New("invalid syllable")

// Reason says why a syllable did not parse.
type Reason string

// Parse failure reasons.
const (
	ReasonEmpty         Reason = "empty"
	ReasonInvalidChar   Reason = "invalid_char"
	ReasonNoVowel       Reason = "no_vowel"
	ReasonUnknownRime   Reason = "unknown_rime"
	ReasonMultipleTones Reason = "multiple_tones"
	ReasonStopCodaTone  Reason = "stop_coda_tone"
)

// SyllableError is returned by every parse failure; it wraps ErrInvalidSyllable.
type SyllableError struct {
	Input  string
	Reason Reason
}

func (e *SyllableError) Error() string {
	return fmt.Sprintf("%v %q: %s", ErrInvalidSyllable, e.Input, e.Reason)
}

func (e *SyllableError) Unwrap() error { return ErrInvalidSyllable }
