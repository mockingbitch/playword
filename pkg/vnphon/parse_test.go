package vnphon

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseSpecTable(t *testing.T) {
	// Spec §7, plus cases ADR-001 decides explicitly.
	cases := []struct {
		in                           string
		onset, medial, nucleus, coda string
		tone                         int
	}{
		{"ngày", "ng", "", AW, "j", 2},
		{"tàn", "t", "", A, "n", 2},
		{"chạy", "ch", "", AW, "j", 6},
		{"án", "", "", A, "n", 3},
		{"đạn", "đ", "", A, "n", 6},
		{"may", "m", "", AW, "j", 1},
		{"mắn", "m", "", AW, "n", 3},
		{"quê", "c", "w", EE, "", 1},
		{"huyền", "h", "w", IE, "n", 2},
		{"giữ", "gi", "", UW, "", 5},
		{"gì", "gi", "", I, "", 2},
		{"hoa", "h", "w", A, "", 1},
		{"tai", "t", "", A, "j", 1},
		{"tây", "t", "", AA, "j", 1},
		{"ương", "", "", UOW, "ng", 1},
		{"xoong", "x", "", O, "ng", 1},
		{"nghiêng", "ng", "", IE, "ng", 1},
		// ADR-001 decisions
		{"ai", "", "", A, "j", 1},
		{"ao", "", "", A, "w", 1},
		{"au", "", "", AW, "w", 1},
		{"âu", "", "", AA, "w", 1},
		{"anh", "", "", A, "nh", 1},
		{"mui", "m", "", U, "j", 1},
		{"huy", "h", "w", I, "", 1},
		{"qui", "c", "w", I, "", 1},
		{"quý", "c", "w", I, "", 3},
		{"quỳnh", "c", "w", I, "nh", 2},
		{"quyết", "c", "w", IE, "t", 3},
		{"qua", "c", "w", A, "", 1},
		{"của", "c", "", UO, "", 4},
		{"quốc", "c", "w", OO, "k", 3},
		{"cuốc", "c", "", UO, "k", 3},
		{"gìn", "gi", "", I, "n", 2},
		{"giết", "gi", "", IE, "t", 3},
		{"giếng", "gi", "", IE, "ng", 3},
		{"gia", "gi", "", A, "", 1},
		{"giữa", "gi", "", UOW, "", 5},
		{"thuở", "th", "w", OW, "", 4},
		{"khuya", "kh", "w", IE, "", 1},
		{"khuỷu", "kh", "w", I, "w", 4},
		{"yêu", "", "", IE, "w", 1},
		{"kỹ", "c", "", I, "", 5},
		{"ghế", "g", "", EE, "", 3},
		{"boong", "b", "", O, "ng", 1},
		{"ách", "", "", A, "ch", 3},
		// Found missing by the M2 corpus check of the HND dictionary.
		{"ngoáo", "ng", "w", A, "w", 3},
		{"ngoém", "ng", "w", E, "m", 3},
		{"Đường", "đ", "", UOW, "ng", 2},
	}
	for _, c := range cases {
		got, err := ParseSyllable(c.in)
		want := Syllable{c.onset, c.medial, c.nucleus, c.coda, c.tone}
		if err != nil || got != want {
			t.Errorf("ParseSyllable(%q) = %+v, %v; want %+v", c.in, got, err, want)
		}
	}
}

func TestParseIgnoresMarkPlacementAndNormalization(t *testing.T) {
	same := [][]string{
		{"hoà", "hòa", "HOÀ", "hòa", "hoà"},
		{"thuỷ", "thủy"},
		{"khoẻ", "khỏe"},
		{"người", "người"},
	}
	for _, group := range same {
		want, err := ParseSyllable(group[0])
		if err != nil {
			t.Fatalf("%q: %v", group[0], err)
		}
		for _, w := range group[1:] {
			if got, err := ParseSyllable(w); err != nil || got != want {
				t.Errorf("ParseSyllable(%q) = %+v, %v; want %+v", w, got, err, want)
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]Reason{
		"":      ReasonEmpty,
		"wifi":  ReasonInvalidChar,
		"z":     ReasonInvalidChar,
		"a1":    ReasonInvalidChar,
		"ng":    ReasonNoVowel,
		"kh":    ReasonNoVowel,
		"tàt":   ReasonStopCodaTone,
		"anh̀́": ReasonMultipleTones,
		"oai̇":  ReasonInvalidChar,
		"tuu":   ReasonUnknownRime,
		"quu":   ReasonUnknownRime,
		"xyz":   ReasonInvalidChar,
		"bôông": ReasonUnknownRime,
		"giia":  ReasonUnknownRime,
		"giiu":  ReasonUnknownRime,
		"quiết": ReasonUnknownRime,
	}
	for in, want := range cases {
		_, err := ParseSyllable(in)
		var se *SyllableError
		if !errors.Is(err, ErrInvalidSyllable) || !errors.As(err, &se) || se.Reason != want {
			t.Errorf("ParseSyllable(%q) err = %v, want reason %s", in, err, want)
		}
	}
	if !strings.Contains((&SyllableError{"x", ReasonEmpty}).Error(), "invalid syllable") {
		t.Error("error message")
	}
}

func TestEveryRimeSpellingParses(t *testing.T) {
	for _, sp := range RimeSpellings() {
		for _, onset := range []string{"", "t", "h"} {
			tone := ""
			r := rimes[sp]
			if IsStopCoda(r.coda) {
				tone = "́"
			}
			w := onset + sp + tone
			s, err := ParseSyllable(w)
			if err != nil {
				t.Errorf("%q: %v", w, err)
				continue
			}
			if s.Medial != r.medial || s.Nucleus != r.nucleus || s.Coda != r.coda {
				t.Errorf("%q parsed as %+v, table says %+v", w, s, r)
			}
		}
	}
	if len(Rimes()) == 0 || len(Rimes()) > len(RimeSpellings()) {
		t.Error("Rimes() inconsistent with RimeSpellings()")
	}
}

// Every word built from a real onset spelling and rime spelling that parses
// must survive Parse → Render → Parse unchanged (spec §7 round-trip).
func TestRoundTrip(t *testing.T) {
	onsetSpellings := append([]string{"", "gi", "qu"}, onsets...)
	n := 0
	for _, o := range onsetSpellings {
		for _, sp := range RimeSpellings() {
			for tone := ToneNgang; tone <= ToneNang; tone++ {
				w := o + sp
				if m, ok := toneRunes[tone]; ok {
					w += string(m)
				}
				s, err := ParseSyllable(w)
				if err != nil {
					continue
				}
				n++
				out, err := Render(s)
				if err != nil {
					t.Fatalf("Render(%+v) from %q: %v", s, w, err)
				}
				back, err := ParseSyllable(out)
				if err != nil || back != s {
					t.Fatalf("%q → %+v → %q → %+v, %v", w, s, out, back, err)
				}
			}
		}
	}
	if n < 5000 {
		t.Fatalf("only %d syllables exercised", n)
	}
}

func TestRenderCanonical(t *testing.T) {
	cases := map[string]string{
		"hoà": "hoà", "hòa": "hoà", "thủy": "thuỷ", "quý": "quý", "ki": "ki", "kì": "kì",
		"ghế": "ghế", "nghiêng": "nghiêng", "gì": "gì", "gìn": "gìn", "giếng": "giếng",
		"yêu": "yêu", "người": "người", "mía": "mía", "của": "của", "quốc": "quốc",
		"khuya": "khuya", "huyền": "huyền", "Ương": "ương", "ly": "li",
	}
	for in, want := range cases {
		s, err := ParseSyllable(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got, err := Render(s); err != nil || got != want {
			t.Errorf("Render(Parse(%q)) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Render(Syllable{Nucleus: AW, Coda: "nh"}); !errors.Is(err, ErrInvalidSyllable) {
		t.Error("rendering a rime outside the inventory must fail")
	}
	if _, err := Render(Syllable{Onset: "c", Medial: MedialW, Nucleus: U}); !errors.Is(err, ErrInvalidSyllable) {
		t.Error("qu + u must fail")
	}
}

func TestPhrase(t *testing.T) {
	got := SplitWords("  Ngày-tàn, (may mắn)!  …  ")
	if strings.Join(got, "|") != "Ngày|tàn|may|mắn" {
		t.Errorf("SplitWords = %q", got)
	}
	p, err := ParsePhrase("ngày tàn")
	if err != nil || len(p) != 2 || p[1].Nucleus != A {
		t.Fatalf("ParsePhrase = %+v, %v", p, err)
	}
	if s, err := RenderPhrase(p); err != nil || s != "ngày tàn" {
		t.Errorf("RenderPhrase = %q, %v", s, err)
	}
	if _, err := RenderPhrase([]Syllable{{Nucleus: "X"}}); err == nil {
		t.Error("RenderPhrase must propagate errors")
	}
	if _, err := ParsePhrase(" ,, "); !errors.Is(err, ErrInvalidSyllable) {
		t.Error("empty phrase must fail")
	}
	if _, err := ParsePhrase("ngày wifi"); !errors.Is(err, ErrInvalidSyllable) {
		t.Error("invalid word must fail the phrase")
	}
}

func FuzzParseSyllable(f *testing.F) {
	for _, s := range []string{"ngày", "quỳnh", "gìn", "hoà", "wifi", "", "ườ́", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		s, err := ParseSyllable(in)
		if err != nil {
			return
		}
		if !utf8.ValidString(in) {
			t.Fatalf("accepted invalid UTF-8 %q", in)
		}
		out, err := Render(s)
		if err != nil {
			t.Fatalf("Render(%+v) from %q: %v", s, in, err)
		}
		if back, err := ParseSyllable(out); err != nil || back != s {
			t.Fatalf("%q → %+v → %q → %+v, %v", in, s, out, back, err)
		}
	})
}
