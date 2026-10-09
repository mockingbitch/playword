package lookup

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Normalize lowercases s, converts it to NFC and collapses whitespace, so
// visually identical input ("à" as one code point or as a + U+0300) compares equal.
func Normalize(s string) string {
	s = norm.NFC.String(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// Fold strips diacritics: "Đường" -> "duong". Each rune of the normalized
// input maps to exactly one rune, so prefix lengths carry over between forms.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(Normalize(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'đ' {
			r = 'd'
		}
		b.WriteRune(r)
	}
	return b.String()
}

var telexTones = map[rune]byte{
	0x0301: 's', // sắc
	0x0300: 'f', // huyền
	0x0309: 'r', // hỏi
	0x0303: 'x', // ngã
	0x0323: 'j', // nặng
}

// Telex spells s the way it is typed without an input method:
// "đường" -> "dduwowngf", "nhà" -> "nhaf". The tone key goes at the end of
// each syllable, which is how most people type it.
func Telex(s string) string {
	var b strings.Builder
	for i, word := range strings.Fields(Normalize(s)) {
		if i > 0 {
			b.WriteByte(' ')
		}
		var tone byte
		var base rune
		for _, r := range norm.NFD.String(word) {
			switch r {
			case 0x0302: // circumflex: â ê ô -> aa ee oo
				b.WriteRune(base)
			case 0x0306, 0x031B: // breve ă, horn ơ ư -> aw ow uw
				b.WriteByte('w')
			case 'đ':
				b.WriteString("dd")
				base = r
			default:
				if t, ok := telexTones[r]; ok {
					tone = t
				} else if !unicode.Is(unicode.Mn, r) {
					b.WriteRune(r)
					base = r
				}
			}
		}
		if tone != 0 {
			b.WriteByte(tone)
		}
	}
	return b.String()
}

// tokenize splits folded text into searchable words.
func tokenize(s string) []string {
	return strings.FieldsFunc(Fold(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// distance is the optimal-string-alignment edit distance (Levenshtein plus
// adjacent transpositions), which covers the typos people actually make.
func distance(a, b []rune) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}
