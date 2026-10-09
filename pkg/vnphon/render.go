package vnphon

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

var toneRunes = map[int]rune{ToneHuyen: '̀', ToneSac: '́', ToneHoi: '̉', ToneNga: '̃', ToneNang: '̣'}

// Render writes s in canonical spelling: k/gh/ngh before front vowels, qu for
// c + medial, the first spelling listed in the rime table, and the tone mark
// on the main vowel. Parse(Render(s)) == s for every s produced by Parse.
func Render(s Syllable) (string, error) {
	fail := &SyllableError{Input: s.Nucleus + "." + s.Coda, Reason: ReasonUnknownRime}
	front := s.Medial == "" && (s.Nucleus == I || s.Nucleus == E || s.Nucleus == EE || s.Nucleus == IE)

	onset, body := s.Onset, ""
	switch {
	case s.Onset == "c" && s.Medial == MedialW:
		b, ok := canonical[rime{"", s.Nucleus, s.Coda}]
		if !ok || !quNuclei[s.Nucleus] {
			return "", fail
		}
		onset, body = "qu", b
		if s.Nucleus == I || s.Nucleus == IE {
			body = "y" + strings.TrimPrefix(body, "i")
		}
	default:
		b, ok := canonical[rime{s.Medial, s.Nucleus, s.Coda}]
		if !ok {
			return "", fail
		}
		body = b
		switch {
		case s.Onset == "gi" && (s.Nucleus == I || s.Nucleus == IE) && s.Medial == "":
			body = strings.TrimPrefix(body, "i")
		case s.Onset == "" && s.Nucleus == IE && s.Medial == "" && s.Coda != "":
			// yêu, yên … but iếc, iêng have no y spelling.
			if y := "y" + strings.TrimPrefix(body, "i"); rimes[y] == (rime{s.Medial, s.Nucleus, s.Coda}) {
				body = y
			}
		}
		if front {
			switch s.Onset {
			case "c":
				onset = "k"
			case "g":
				onset = "gh"
			case "ng":
				onset = "ngh"
			}
		}
	}

	r := []rune(onset + body)
	if mark, ok := toneRunes[s.Tone]; ok {
		// -1 (no vowel in the rime: gì, gìn) lands on the i of gi.
		pos := tonePosition([]rune(body), s.Medial == MedialW && onset != "qu") + len([]rune(onset))
		r = append(r[:pos+1], append([]rune{mark}, r[pos+1:]...)...)
	}
	return norm.NFC.String(string(r)), nil
}

// tonePosition picks the main vowel of a rime spelling: the last vowel with a
// quality mark (ươ → ơ, iê → ê), otherwise the first vowel after the medial
// letter (hoà, thuý, mía, múa). It returns -1 when the rime has no vowel.
func tonePosition(body []rune, skipMedial bool) int {
	for i := len(body) - 1; i >= 0; i-- {
		if strings.ContainsRune("ăâêôơư", body[i]) {
			return i
		}
	}
	start := 0
	if skipMedial {
		start = 1
	}
	for i := start; i < len(body); i++ {
		if strings.ContainsRune(vowels, body[i]) {
			return i
		}
	}
	return -1
}

// RenderPhrase renders each syllable and joins them with spaces.
func RenderPhrase(p []Syllable) (string, error) {
	words := make([]string, len(p))
	for i, s := range p {
		w, err := Render(s)
		if err != nil {
			return "", err
		}
		words[i] = w
	}
	return strings.Join(words, " "), nil
}
