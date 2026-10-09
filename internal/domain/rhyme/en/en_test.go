package en

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

var dict = map[string][]string{
	"money": ParsePhones("M AH1 N IY0"), "honey": ParsePhones("HH AH1 N IY0"),
	"funny": ParsePhones("F AH1 N IY0"), "sunny": ParsePhones("S AH1 N IY0"),
	"bunny": ParsePhones("B AH1 N IY0"), "tummy": ParsePhones("T AH1 M IY0"),
	"mono": ParsePhones("M OW1 N OW0"), "muddy": ParsePhones("M AH1 D IY0"),
	"company": ParsePhones("K AH1 M P AH0 N IY0"),
	"get":     ParsePhones("G EH1 T"), "pet": ParsePhones("P EH1 T"),
	"stop": ParsePhones("S T AA1 P"), "bake": ParsePhones("B EY1 K"),
	"wish": ParsePhones("W IH1 SH"), "play": ParsePhones("P L EY1"),
	"run": ParsePhones("R AH1 N"), "kind": ParsePhones("K AY1 N D"),
	"want": ParsePhones("W AA1 N T"), "happy": ParsePhones("HH AE1 P IY0"),
	"dark": ParsePhones("D AA1 R K"), "fast": ParsePhones("F AE1 S T"),
}

func segs(t *testing.T, phrase string) []Seg {
	t.Helper()
	ph, _, err := NewPronouncer(dict).Pronounce(phrase)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Segments(ph)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// phraseLevel compares the last n segments; the weakest segment decides.
func phraseLevel(a, b []Seg, n int) (Level, bool) {
	worst := Perfect
	for i := 1; i <= n; i++ {
		l, ok := SegLevel(a[len(a)-i], b[len(b)-i])
		if !ok {
			return 0, false
		}
		worst = max(worst, l)
	}
	return worst, true
}

// Spec §7 EN, with the I9 correction (sunny and bunny are perfect rhymes).
func TestSpecMoney(t *testing.T) {
	money := segs(t, "money")
	cases := []struct {
		word string
		want Level
		ok   bool
	}{
		{"honey", Perfect, true}, {"funny", Perfect, true}, {"sunny", Perfect, true}, {"bunny", Perfect, true},
		{"tummy", Family, true}, // N ~ M: both nasal
		{"muddy", Assonance, true},
		{"company", Family, true}, // same sounds, but its AH is unstressed
		{"mono", 0, false},
	}
	for _, c := range cases {
		got, ok := phraseLevel(money, segs(t, c.word), 2)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("money ~ %s = %v/%v, want %v/%v", c.word, got, ok, c.want, c.ok)
		}
	}
	// Multi-syllable: "get money" ~ "pet honey" on three vowels (assonance), two of them perfect.
	a, b := segs(t, "get money"), segs(t, "pet honey")
	if l, ok := phraseLevel(a, b, 3); !ok || l != Assonance {
		t.Errorf("get money ~ pet honey (3) = %v/%v", l, ok)
	}
	if l, ok := phraseLevel(a, b, 2); !ok || l != Perfect {
		t.Errorf("get money ~ pet honey (2) = %v/%v", l, ok)
	}
}

func TestKeysAgreeWithSegLevel(t *testing.T) {
	var all []Seg
	for w := range dict {
		all = append(all, segs(t, w)...)
	}
	for _, a := range all {
		for _, b := range all {
			want, wantOK := Level(0), false
			for _, l := range Levels {
				if Key(l, a) == Key(l, b) {
					want, wantOK = l, true
					break
				}
			}
			got, ok := SegLevel(a, b)
			if ok != wantOK || (ok && got != want) {
				t.Errorf("%v vs %v: SegLevel %v/%v, keys %v/%v", a, b, got, ok, want, wantOK)
			}
		}
	}
	if PhraseKey(Perfect, segs(t, "money")) != "'AH.N IY." || PhraseKey(Family, segs(t, "money")) != "@.N I." {
		t.Errorf("PhraseKey = %q", PhraseKey(Family, segs(t, "money")))
	}
}

func TestDerivedPronunciations(t *testing.T) {
	p := NewPronouncer(dict)
	cases := map[string]string{
		"stopped": "S T AA1 P T", "baked": "B EY1 K T", "wanted": "W AA1 N T IH0 D", "played": "P L EY1 D",
		"running": "R AH1 N IH0 NG", "baking": "B EY1 K IH0 NG", "wishes": "W IH1 SH IH0 Z",
		"gets": "G EH1 T S", "pets": "P EH1 T S", "plays": "P L EY1 Z", "money's": "M AH1 N IY0 Z",
		"monies": "M AH1 N IY0 Z", "kindness": "K AY1 N D N AH0 S", "kindly": "K AY1 N D L IY0",
		"darker": "D AA1 R K ER0", "fastest": "F AE1 S T AH0 S T",
	}
	for w, want := range cases {
		ph, conf, err := p.Pronounce(w)
		if err != nil || strings.Join(ph, " ") != want || conf != DerivedConfidence {
			t.Errorf("%s = %v %v %v, want %s", w, ph, conf, err, want)
		}
	}
	if _, conf, _ := p.Pronounce("Money"); conf != 1 {
		t.Error("dictionary words have confidence 1")
	}
	if _, _, err := p.Pronounce("xyzzy"); err == nil {
		t.Error("unknown word must fail")
	}
	if _, _, err := p.Pronounce(" - "); err == nil {
		t.Error("empty phrase must fail")
	}
}

func TestSegments(t *testing.T) {
	s, err := Segments(ParsePhones("S T AA1 P"))
	if err != nil || len(s) != 1 || s[0].Vowel != "AA" || s[0].Stress != 1 || !slices.Equal(s[0].Tail, []string{"P"}) {
		t.Fatalf("%+v %v", s, err)
	}
	if s[0].String() != "AA1 P" {
		t.Errorf("String = %q", s[0].String())
	}
	back, err := SegmentsFromRows([][]string{s[0].Phones()})
	if err != nil || back[0].Vowel != "AA" {
		t.Errorf("round trip: %+v %v", back, err)
	}
	if _, err := SegmentsFromRows([][]string{{"P"}}); err == nil {
		t.Error("a stored row without a vowel is invalid")
	}
	if _, err := Segments(ParsePhones("HH M")); !errors.Is(err, ErrNoVowel) {
		t.Errorf("no vowel: %v", err)
	}
	if _, err := Segments([]string{"Q1"}); err == nil {
		t.Error("unknown phone must fail")
	}
	if s, _ := Segments([]string{"AH"}); s[0].Stress != 0 {
		t.Error("missing stress digit means unstressed")
	}
	if l, ok := ParseLevel("Family"); !ok || l != Family || l.String() != "family" {
		t.Error("ParseLevel")
	}
	if _, ok := ParseLevel("L0"); ok {
		t.Error("VI levels are not English levels")
	}
}
