package vnphon

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var toneMarks = map[rune]int{
	'̀': ToneHuyen,
	'́': ToneSac,
	'̉': ToneHoi,
	'̃': ToneNga,
	'̣': ToneNang,
}

const (
	letters = "abcdeghiklmnopqrstuvxyăâđêôơư"
	vowels  = "aăâeêioôơuưy"
)

// Onsets other than "gi" and "qu", longest first so prefix matching is greedy.
var onsets = []string{
	"ngh", "ng", "nh", "ch", "gh", "kh", "ph", "th", "tr",
	"b", "c", "d", "đ", "g", "h", "k", "l", "m", "n", "p", "r", "s", "t", "v", "x",
}

var onsetNorm = map[string]string{"k": "c", "gh": "g", "ngh": "ng"}

// ParseSyllable parses one written syllable. Case, Unicode normalization and
// tone mark placement (hoà / hòa) do not matter.
func ParseSyllable(word string) (Syllable, error) {
	fail := func(r Reason) (Syllable, error) {
		return Syllable{}, &SyllableError{Input: word, Reason: r}
	}
	if word == "" {
		return fail(ReasonEmpty)
	}
	body, tone, reason := splitTone(word)
	if reason != "" {
		return fail(reason)
	}
	// After gi an i is always merged into the onset's (gì, giết); "gii…" or
	// "giy…" would render ambiguously (giiu → giu) and is not a word.
	if strings.HasPrefix(body, "gii") || strings.HasPrefix(body, "giy") {
		return fail(ReasonUnknownRime)
	}
	onset, rest := splitOnset(body)
	if !strings.ContainsAny(rest, vowels) {
		return fail(ReasonNoVowel)
	}

	var r rime
	var ok bool
	if onset == "qu" {
		onset = "c"
		r, ok = quRime(rest)
	} else {
		r, ok = rimes[rest]
	}
	if !ok {
		return fail(ReasonUnknownRime)
	}
	if IsStopCoda(r.coda) && tone != ToneSac && tone != ToneNang {
		return fail(ReasonStopCodaTone)
	}
	if n, ok := onsetNorm[onset]; ok {
		onset = n
	}
	return Syllable{Onset: onset, Medial: r.medial, Nucleus: r.nucleus, Coda: r.coda, Tone: tone}, nil
}

// splitTone lowercases word, removes its tone mark and returns the bare
// letters in NFC. The tone is found wherever the mark was placed.
func splitTone(word string) (string, int, Reason) {
	tone := ToneNgang
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(word)) {
		if t, ok := toneMarks[r]; ok {
			if tone != ToneNgang {
				return "", 0, ReasonMultipleTones
			}
			tone = t
			continue
		}
		b.WriteRune(r)
	}
	body := norm.NFC.String(b.String())
	for _, r := range body {
		if !strings.ContainsRune(letters, r) {
			return "", 0, ReasonInvalidChar
		}
	}
	return body, tone, ""
}

func splitOnset(s string) (onset, rest string) {
	if rest, ok := strings.CutPrefix(s, "gi"); ok {
		// gì, gìn: the i is the nucleus. giết, giếng: gi + iê written with one i.
		if !strings.ContainsAny(rest, vowels) || strings.HasPrefix(rest, "ê") {
			return "gi", "i" + rest
		}
		return "gi", rest
	}
	if rest, ok := strings.CutPrefix(s, "qu"); ok && strings.ContainsAny(rest, vowels) {
		return "qu", rest
	}
	for _, o := range onsets {
		if rest, ok := strings.CutPrefix(s, o); ok {
			return o, rest
		}
	}
	return "", s
}

// quRime resolves what follows "qu": the u is the medial, i is written y
// (quy, quýt, quỳnh; the old spelling qui is accepted) and iê only as yê
// (quyết, quyền).
func quRime(rest string) (rime, bool) {
	writtenI := strings.HasPrefix(rest, "i")
	if strings.HasPrefix(rest, "y") && !strings.HasPrefix(rest, "yê") {
		rest = "i" + strings.TrimPrefix(rest, "y")
	}
	r, ok := rimes[rest]
	if !ok || r.medial != "" || !quNuclei[r.nucleus] || (writtenI && r.nucleus == IE) {
		return rime{}, false
	}
	r.medial = MedialW
	return r, true
}

// SplitWords breaks a phrase into syllable-sized words on spaces and hyphens,
// dropping surrounding punctuation.
func SplitWords(phrase string) []string {
	fields := strings.FieldsFunc(phrase, func(r rune) bool {
		return unicode.IsSpace(r) || r == '-' || r == '‐' || r == '–'
	})
	out := fields[:0]
	for _, f := range fields {
		f = strings.TrimFunc(f, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ParsePhrase parses every syllable of phrase; it fails on the first invalid one.
func ParsePhrase(phrase string) ([]Syllable, error) {
	words := SplitWords(phrase)
	if len(words) == 0 {
		return nil, &SyllableError{Input: phrase, Reason: ReasonEmpty}
	}
	out := make([]Syllable, len(words))
	for i, w := range words {
		s, err := ParseSyllable(w)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

// OnsetLen is how many leading letters of a written syllable spell its
// onset, for highlighting the rhyming part: 2 for "ngày", 1 for "quân"
// (the u is the medial, part of the rime) and for "giết" and "gì" (the i
// is shared with the rime), 0 for "an". It is 0 when word is not a
// Vietnamese syllable.
func OnsetLen(word string) int {
	if _, err := ParseSyllable(word); err != nil {
		return 0
	}
	body, _, _ := splitTone(word)
	onset, rest := splitOnset(body)
	n := utf8.RuneCountInString(onset)
	switch {
	case onset == "qu":
		return 1
	case onset == "gi" && utf8.RuneCountInString(rest) == utf8.RuneCountInString(body)-1:
		return 1 // the i was put back into the rime
	}
	return n
}

// WrittenRime is the rime of a written syllable without its tone mark, as a
// rapper would name it: "ay" for "ngày", "uân" for "quân", "iêt" for
// "giết". ok is false when word is not a Vietnamese syllable.
func WrittenRime(word string) (rime string, ok bool) {
	if _, err := ParseSyllable(word); err != nil {
		return "", false
	}
	body, _, _ := splitTone(word)
	return string([]rune(body)[OnsetLen(word):]), true
}
