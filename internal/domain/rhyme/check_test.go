package rhyme

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// colours maps each coloured chain to its group letter, "hải quân" -> "A".
func colours(r CheckResult) map[string]string {
	out := map[string]string{}
	var cur []string
	g, line := 0, -1
	flush := func() {
		if g > 0 && len(cur) > 0 {
			out[strings.Join(cur, " ")] = r.groupByID(g).Letter
		}
		cur = nil
	}
	for _, t := range r.Tokens {
		if t.Group != g || t.Line != line {
			flush()
			g, line = t.Group, t.Line
		}
		if t.Group > 0 {
			cur = append(cur, t.Text)
		}
	}
	flush()
	return out
}

func check(t *testing.T, text string, level Level) CheckResult {
	t.Helper()
	return repoEngine(t).Check(text, CheckOptions{Dialect: Bac, Level: level})
}

func TestCheckStrictestPartnerWins(t *testing.T) {
	// The user's example: hải quân ~ hai tuần, ngày tàn ~ chạy án. At rap-flex
	// the two pairs also rhyme with each other, but each has a stricter partner.
	text := "Đêm nay phố vắng ngày tàn\nĐứng nhìn hải quân đi qua\nChạy án cả một đời ta\nMỗi hai tuần lại xa một người"
	r := check(t, text, L25)
	c := colours(r)
	if c["ngày tàn"] == "" || c["ngày tàn"] != c["Chạy án"] {
		t.Errorf("ngày tàn ~ chạy án: %v", c)
	}
	if c["hải quân"] == "" || c["hải quân"] != c["hai tuần"] {
		t.Errorf("hải quân ~ hai tuần: %v", c)
	}
	if c["ngày tàn"] == c["hải quân"] {
		t.Errorf("two groups must keep two colours: %v", c)
	}
	if len(r.Groups) != 2 || r.Groups[0].Letter != "A" || r.Groups[0].Label != "ay-an" || r.Groups[1].Label != "ai-uân" {
		t.Errorf("groups: %+v", r.Groups)
	}
	if r.Groups[0].Level != L2 || r.Groups[0].Chains != 2 {
		t.Errorf("group A: %+v", r.Groups[0])
	}
}

func TestCheckSingleSyllablesAndLooseLevel(t *testing.T) {
	// one-syllable rhymes are not coloured
	r := check(t, "Một mình ngồi giữa ngày tàn\nnhớ thương đàn\n", L25)
	if len(r.Groups) != 0 {
		t.Errorf("single syllables: %+v", r.Groups)
	}
	// a chain that only has a rap-flex partner joins exactly one group
	text := "phố vắng ngày tàn\nchạy án đời ta\nhải quân xa xa\nhai tuần không về\ntài sản còn đâu"
	r = check(t, text, L25)
	c := colours(r)
	if c["hải quân"] == "" || c["hải quân"] != c["hai tuần"] {
		t.Errorf("the strict pair keeps its colour against a longer loose chain: %v", c)
	}
	if c["tài sản"] == "" || (c["tài sản"] != c["ngày tàn"] && c["tài sản"] != c["hải quân"]) || len(r.Groups) != 2 {
		t.Errorf("a loose chain attaches to one existing group: %v %+v", c, r.Groups)
	}
	// at a strict level the loose pair is not a rhyme at all
	if r := check(t, "ngày tàn\nhải quân", L2); len(r.Groups) != 0 {
		t.Errorf("L2 keeps ay/ai apart: %+v", r.Groups)
	}
	if r := check(t, "ngày tàn\nhải quân", L25); len(r.Groups) != 1 {
		t.Errorf("L25 joins them: %+v", r.Groups)
	}
}

func TestCheckRepeatsAndStopwords(t *testing.T) {
	r := check(t, "đời ta là ngày tàn\nngày tàn của đời ta", L25)
	if len(r.Groups) != 0 {
		t.Errorf("a phrase said again is not a rhyme: %+v", r.Groups)
	}
	reps := 0
	for _, tk := range r.Tokens {
		if tk.Repeat {
			reps++
		}
	}
	if reps < 4 || !r.Lines[0].Repeat && !r.Lines[1].Repeat && r.Tokens[3].Repeat == false {
		t.Errorf("repeats marked: %d %+v", reps, r.Lines)
	}
	// line ends on the same word: a repeat, not a rhyme
	r = check(t, "em đi về\nanh ngồi về", L25)
	if !r.Lines[0].Repeat || !r.Lines[1].Repeat || len(r.Groups) != 0 {
		t.Errorf("same last word: %+v", r.Lines)
	}
	// a chain made only of function words carries nothing
	if r := check(t, "rồi là mây\nthôi và bay", L25); len(r.Groups) != 1 || colours(r)["rồi là"] != "" {
		t.Errorf("stopword-only chain: %v %+v", colours(r), r.Groups)
	}
}

func TestCheckStanzas(t *testing.T) {
	// rhymes do not reach across a blank line
	r := check(t, "phố vắng ngày tàn\n\nchạy án đời ta", L25)
	if len(r.Groups) != 0 || r.Lines[1].Stanza != -1 || r.Lines[2].Stanza != 1 {
		t.Errorf("stanzas: %+v %+v", r.Groups, r.Lines)
	}
	if got := strings.Join(check(t, "phố vắng ngày tàn\nchạy án đời ta\nbuồn chạy án\n\nmột hai\n", L25).Schemes(), "|"); got != "A·A|·" {
		t.Errorf("schemes = %q", got)
	}
}

func TestCheckInternalRhyme(t *testing.T) {
	// two chains in one line, not overlapping
	r := check(t, "ngày tàn ta chạy án", L25)
	c := colours(r)
	if c["ngày tàn"] == "" || c["ngày tàn"] != c["chạy án"] {
		t.Errorf("internal: %v", c)
	}
}

func TestCheckOffsetsAndOddText(t *testing.T) {
	text := "🔥 Đêm nay — ngày-tàn!\r\nchạy án, wifi 2026"
	r := check(t, text, L25)
	u := utf16.Encode([]rune(text))
	for _, tk := range r.Tokens {
		if got := string(utf16.Decode(u[tk.Start:tk.End])); got != tk.Text {
			t.Errorf("offsets of %q give %q", tk.Text, got)
		}
	}
	if c := colours(r); c["ngày tàn"] == "" {
		t.Errorf("hyphen splits words: %v", c)
	}
	for _, tk := range r.Tokens {
		if tk.Text == "wifi" && (tk.Syllable || tk.Group != 0) {
			t.Error("wifi is not a syllable")
		}
	}
	for _, s := range []string{"", "\n\n\n", "!!!", strings.Repeat("a ", 3)} {
		_ = check(t, s, L3) // never panics
	}
}

func TestCheckDialect(t *testing.T) {
	// southern and central speech merge -n and -ng here: a perfect rhyme
	// there, none in the north
	text := "ngồi bên tàn cây\nđứng gần hàng mây"
	for d, want := range map[Dialect]int{Bac: 0, Nam: 1, Trung: 1} {
		if r := repoEngine(t).Check(text, CheckOptions{Dialect: d, Level: L0}); len(r.Groups) != want {
			t.Errorf("%s: %d groups, want %d: %+v", d, len(r.Groups), want, r.Groups)
		}
	}
}

func TestCheckDensity(t *testing.T) {
	r := check(t, "phố vắng ngày tàn\nchạy án đời ta", L25)
	if r.Density < 2 || r.Rhymed <= 0 || r.Lines[1].LongestRun < 2 {
		t.Errorf("density %v rhymed %v lines %+v", r.Density, r.Rhymed, r.Lines)
	}
}

func FuzzCheck(f *testing.F) {
	for _, s := range []string{"ngày tàn\nchạy án", "a\n\nb", "🔥", "qu gi gì", "Bàn tay trắng\nNgày tàn hay là ngày tàn\nTay trắng"} {
		f.Add(s)
	}
	e := repoEngine(&testing.T{})
	f.Fuzz(func(t *testing.T, s string) {
		r := e.Check(s, CheckOptions{Dialect: Nam, Level: L3})
		noFragments(t, r)
		n := len(utf16.Encode([]rune(s)))
		for _, tk := range r.Tokens {
			if tk.Start < 0 || tk.End > n || tk.Start >= tk.End {
				t.Fatalf("bad offsets %+v in %q", tk, s)
			}
		}
	})
}

func BenchmarkCheck(b *testing.B) {
	e := repoEngine(&testing.T{})
	verse := "Đêm nay phố vắng ngày tàn\nĐứng nhìn hải quân đi qua\nChạy án cả một đời ta\nMỗi hai tuần lại xa một người\n"
	text := strings.Repeat(verse+strings.Repeat("ta đi trong mưa giữa thành phố đêm khuya vắng\n", 4)+"\n", 60) // ≈ 20k chars
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		e.Check(text, CheckOptions{Dialect: Bac, Level: L25})
	}
}

func BenchmarkCheckVerse(b *testing.B) {
	e := repoEngine(&testing.T{})
	text := strings.Repeat("Đêm nay phố vắng ngày tàn\nĐứng nhìn hải quân đi qua\nChạy án cả một đời ta\nMỗi hai tuần lại xa một người\n", 4) // 16 lines
	b.SetBytes(int64(len(text)))
	for range b.N {
		e.Check(text, CheckOptions{Dialect: Bac, Level: L25})
	}
}

// noFragments: every coloured run of words is at least two syllables long.
func noFragments(t *testing.T, r CheckResult) {
	t.Helper()
	for _, ln := range r.Lines {
		toks := r.Tokens[ln.From:ln.To]
		for i := 0; i < len(toks); {
			j := i
			for j < len(toks) && toks[j].Group == toks[i].Group {
				j++
			}
			if toks[i].Group > 0 && j-i < 2 {
				t.Errorf("one-syllable fragment %q in group %d", toks[i].Text, toks[i].Group)
			}
			i = j
		}
	}
}

func TestCheckNoFragments(t *testing.T) {
	text := "Tài sản để lại cho ai\nBàn tay trắng bước ra ngoài cửa ngày mai\nNgày tàn hay là ngày tàn\nTay trắng mà lòng không sang"
	for _, l := range Levels {
		noFragments(t, check(t, text, l))
	}
	noFragments(t, check(t, "Đêm nay phố vắng ngày tàn\nĐứng nhìn hải quân đi qua\nChạy án cả một đời ta\nMỗi hai tuần lại xa một người", L3))
}
