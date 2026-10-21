package rhyme

import (
	"errors"

	"math"
	"os"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme/en"
	"strings"
	"testing"
	"time"

	"playword/pkg/vnphon"
)

const testFamilies = `
version: 1
nucleus_families:
  A*:  [A, AW, AA]
  E*:  [E, EE]
  I*:  [I, IE]
  O*:  [O, OO]
  U*:  [U, UO]
  OW*: [OW, UW, UOW]
coda_families:
  N*: [n, ng, nh, m]
  T*: [t, k, ch, p]
  J*: [j]
  W*: [w]
  "Ø*": [""]
cross_coda_equivalents:
  - [j, n]
  - [w, m]
`

func families(t *testing.T, yaml string) *FamiliesConfig {
	t.Helper()
	f, err := LoadFamilies(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func syl(t testing.TB, w string) vnphon.Syllable {
	t.Helper()
	s, err := vnphon.ParseSyllable(w)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestKeys(t *testing.T) {
	k, err := NewKeyer(families(t, testFamilies))
	if err != nil {
		t.Fatal(err)
	}
	tan := syl(t, "tàn")
	want := map[Level]string{L0: "A.n.2", L1: "A.n.B", L2: "A.n", L25: "A*.j+n", L3: "J*+N*+W*"}
	for l, w := range want {
		got := k.Key(l, tan)
		if l == L3 {
			got = strings.TrimPrefix(got, "A*.")
		}
		if got != w {
			t.Errorf("Key(%s, tàn) = %q, want %q", l, got, w)
		}
	}
	if got := k.Key(L1, syl(t, "tán")); got != "A.n.T" {
		t.Errorf("tán L1 = %q", got)
	}
	if got := k.Key(L0, syl(t, "hoa")); got != "A._.1" {
		t.Errorf("open coda L0 = %q", got)
	}
	if got := k.PhraseKey(L2, []vnphon.Syllable{syl(t, "ngày"), tan}); got != "AW.j A.n" {
		t.Errorf("PhraseKey = %q", got)
	}
	// Cross pairs are transitive in merged L3: j~n and w~m pull J*, N*, W* together.
	classes := k.CodaClasses(L3)
	if len(classes["J*+N*+W*"]) != 6 || len(classes["T*"]) != 4 || len(classes["Ø*"]) != 1 {
		t.Errorf("L3 classes = %v", classes)
	}
	if got := k.CodaClasses(L25)["j+n"]; len(got) != 2 {
		t.Errorf("L2.5 classes = %v", k.CodaClasses(L25))
	}

	f := families(t, testFamilies+"l3_coda_mode: families\n")
	k2, err := NewKeyer(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := k2.Key(L3, tan); got != "A*.N*" {
		t.Errorf("families-mode L3 = %q", got)
	}
}

func TestLevelsAndParsers(t *testing.T) {
	for _, l := range Levels {
		got, err := ParseLevel(l.String())
		if err != nil || got != l {
			t.Errorf("ParseLevel(%s) = %v, %v", l, got, err)
		}
		if b, _ := l.MarshalText(); string(b) != l.String() {
			t.Errorf("MarshalText(%s) = %s", l, b)
		}
	}
	if l, err := ParseLevel("l2.5"); err != nil || l != L25 {
		t.Error("L2.5 alias")
	}
	if Level(9).String() != "L?9" {
		t.Error("unknown level string")
	}
	for _, bad := range []func() error{
		func() error { _, err := ParseLevel("L4"); return err },
		func() error { _, err := ParseDialect("hue"); return err },
		func() error { _, err := ParseAlign("middle"); return err },
	} {
		if err := bad(); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("want ErrInvalidQuery, got %v", err)
		}
	}
	if d, err := ParseDialect("NAM"); err != nil || d != Nam {
		t.Error("ParseDialect case-insensitive")
	}
	if a, err := ParseAlign("any"); err != nil || a != AlignAny {
		t.Error("ParseAlign")
	}
}

func TestFamiliesValidation(t *testing.T) {
	replace := func(from, to string) string { return strings.Replace(testFamilies, from, to, 1) }
	cases := map[string]string{
		"unknown nucleus": replace("[A, AW, AA]", "[A, AW, AA, XX]"),
		"duplicate":       replace("[E, EE]", "[E, EE, A]"),
		"missing":         replace("[E, EE]", "[E]"),
		"bad coda":        replace("[j]", "[j, q]"),
		"bad cross":       replace("[j, n]", "[j, n, m]"),
		"cross code":      replace("[w, m]", "[w, x]"),
		"mode":            testFamilies + "l3_coda_mode: loose\n",
	}
	for name, y := range cases {
		if _, err := NewKeyer(families(t, y)); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := LoadFamilies(strings.NewReader("nucleus_familes: {}")); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("typo in key must be rejected, got %v", err)
	}
}

func TestDialectValidation(t *testing.T) {
	cases := map[string]DialectRules{
		"unknown nucleus":    {NucleusRules: []NucleusRule{{WhenCoda: []string{"n"}, From: "A", To: "Q"}}},
		"no when_coda":       {NucleusRules: []NucleusRule{{From: "A", To: "AW"}}},
		"bad when_coda":      {NucleusRules: []NucleusRule{{WhenCoda: []string{"x"}, From: "A", To: "AW"}}},
		"nucleus conflict":   {NucleusRules: []NucleusRule{{WhenCoda: []string{"n"}, From: "A", To: "AW"}, {WhenCoda: []string{"n"}, From: "A", To: "AA"}}},
		"bad coda":           {CodaRules: []CodaRule{{From: "n", To: "x"}}},
		"bad coda nucleus":   {CodaRules: []CodaRule{{From: "n", To: "ng", UnlessNucleus: []string{"Q"}}}},
		"coda conflict":      {CodaRules: []CodaRule{{From: "n", To: "ng"}, {From: "n", To: "m", WhenNucleus: []string{"A"}}}},
		"tone range":         {ToneMerge: map[int]int{7: 1}},
		"tone chain":         {ToneMerge: map[int]int{5: 4, 4: 6}},
		"coda chain":         {CodaRules: []CodaRule{{From: "nh", To: "n"}, {From: "n", To: "ng"}}},
		"nucleus then coda ": {NucleusRules: []NucleusRule{{WhenCoda: []string{"n"}, From: "A", To: "AW"}, {WhenCoda: []string{"n"}, From: "AW", To: "AA"}}},
	}
	for name, r := range cases {
		if _, err := NewMapper("x", r); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	_, err := NewMapper("x", cases["coda chain"])
	if err == nil || !strings.Contains(err.Error(), "idempotent") || !strings.Contains(err.Error(), "→") {
		t.Errorf("idempotency error should show an example, got %v", err)
	}
}

func TestDialectMapperSemantics(t *testing.T) {
	// Conditions look at the original syllable: the coda rule sees nucleus A
	// even though the nucleus rule already turned it into AW.
	m, err := NewMapper("x", DialectRules{
		NucleusRules: []NucleusRule{{WhenCoda: []string{"nh"}, From: "A", To: "AW"}},
		CodaRules:    []CodaRule{{From: "nh", To: "ng", WhenNucleus: []string{"A"}}},
		ToneMerge:    map[int]int{5: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := m.Map(syl(t, "anh"))
	if got.Nucleus != vnphon.AW || got.Coda != "ng" {
		t.Errorf("anh → %+v, want AW.ng", got)
	}
	if got := m.Map(syl(t, "bão")); got.Tone != vnphon.ToneHoi {
		t.Errorf("bão tone → %d", got.Tone)
	}
	if got := m.Map(syl(t, "inh")); got.Coda != "nh" {
		t.Errorf("when_nucleus must restrict: inh → %+v", got)
	}
}

func TestEngineErrors(t *testing.T) {
	if _, err := LoadDir("does-not-exist"); err == nil {
		t.Error("missing dir must fail")
	}
	bad := &DialectsConfig{Nam: DialectRules{ToneMerge: map[int]int{0: 1}}}
	if _, err := NewEngine(families(t, testFamilies), bad); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("bad dialect: %v", err)
	}
	if _, err := NewEngine(families(t, "nucleus_families: {}"), &DialectsConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("bad families: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, dir+"/rhyme_families.yaml", testFamilies)
	writeFile(t, dir+"/dialects.yaml", "bac: {}\nnam: {tone_merge: {5: 4}}\nunknown: 1\n")
	if _, err := LoadDir(dir); !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "dialects.yaml") {
		t.Errorf("unknown key in dialects.yaml: %v", err)
	}
	writeFile(t, dir+"/rhyme_families.yaml", "version: [")
	if _, err := LoadDir(dir); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("broken families yaml: %v", err)
	}
}

func TestSearchOptions(t *testing.T) {
	e, cands := repoEngine(t), fixture(t)
	p, _ := vnphon.ParsePhrase("ngày tàn")

	invalid := []func(*Query){
		func(q *Query) { q.Syllables = nil },
		func(q *Query) { q.Dialect = "hue" },
		func(q *Query) { q.Level = 7 },
		func(q *Query) { q.MinSyl = 0 },
		func(q *Query) { q.MaxSyl = 6 },
		func(q *Query) { q.Limit = 0 },
		func(q *Query) { q.Match = "middle" },
	}
	for i, mod := range invalid {
		q := NewQuery(p)
		mod(&q)
		if _, err := e.Search(q, cands); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}

	q := NewQuery(p)
	q.Limit = 5
	resp, err := e.Search(q, cands)
	if err != nil || len(resp.Results) != 5 {
		t.Fatalf("limit: %d results, %v", len(resp.Results), err)
	}
	for i := 1; i < len(resp.Results); i++ {
		if better(resp.Results[i-1], resp.Results[i]) > 0 {
			t.Fatalf("results not in ranking order at %d: %+v before %+v", i, resp.Results[i-1], resp.Results[i])
		}
	}
	if resp.Results[0].Matched != 2 {
		t.Errorf("2-syllable matches must rank first, got %+v", resp.Results[0])
	}

	// min_syl=2 drops single-syllable rhymes.
	q = NewQuery(p)
	q.MinSyl, q.Limit = 2, 1000
	resp, _ = e.Search(q, cands)
	for _, r := range resp.Results {
		if r.Matched < 2 {
			t.Errorf("min_syl=2 returned %q", r.Candidate.Text)
		}
	}

	// prefix / any alignment: "ngày mai tươi sáng" rhymes on "ngày" at its start.
	ngay, _ := vnphon.ParsePhrase("ngày")
	got := func(a Align) map[string]Result {
		q := NewQuery(ngay)
		q.Match, q.Limit, q.Level = a, 1000, L0
		resp, _ := e.Search(q, cands)
		m := map[string]Result{}
		for _, r := range resp.Results {
			m[r.Candidate.Text] = r
		}
		return m
	}
	if _, ok := got(AlignSuffix)["ngày mai tươi sáng"]; ok {
		t.Error("suffix must not match at the start")
	}
	if r, ok := got(AlignPrefix)["ngày mai tươi sáng"]; !ok || r.Start != 0 || r.End != 1 {
		t.Errorf("prefix: %+v", r)
	}
	if r, ok := got(AlignAny)["tháng ngày"]; !ok || r.Start != 1 {
		t.Errorf("any: %+v", r)
	}
	if r, ok := got(AlignAny)["ngày mai"]; !ok || r.Start != 0 {
		t.Errorf("any should find a window at the start too: %+v", r)
	}

	// Slang filter.
	nhieu, _ := vnphon.ParsePhrase("nhiều")
	q = NewQuery(nhieu)
	q.Dialect, q.Level, q.Limit = Nam, L0, 100
	resp, _ = e.Search(q, cands)
	if !containsText(resp, "nhìu") {
		t.Error("nam: nhiều ≈ nhìu")
	}
	q.ExcludeSlang = true
	resp, _ = e.Search(q, cands)
	if containsText(resp, "nhìu") {
		t.Error("ExcludeSlang must drop slang")
	}

	// Proper-noun filter.
	tan, _ := vnphon.ParsePhrase("tàn")
	proper := []Candidate{{ID: 1, Text: "Lan", Syllables: []vnphon.Syllable{syl(t, "Lan")}, IsProper: true}}
	q = NewQuery(tan)
	if resp, _ := e.Search(q, proper); len(resp.Results) != 1 {
		t.Error("proper nouns are included by default")
	}
	q.ExcludeProper = true
	if resp, _ := e.Search(q, proper); len(resp.Results) != 0 {
		t.Error("ExcludeProper must drop proper nouns")
	}

	// Proper nouns rank after ordinary words with the same number of matched
	// syllables, even at a stricter level or with a higher frequency…
	mixed := []Candidate{
		{ID: 1, Text: "Tàn", Syllables: []vnphon.Syllable{syl(t, "tàn")}, IsProper: true, Zipf: 8},
		{ID: 2, Text: "tán", Syllables: []vnphon.Syllable{syl(t, "tán")}},
		{ID: 3, Text: "may mắn", Syllables: []vnphon.Syllable{syl(t, "may"), syl(t, "mắn")}},
		{ID: 4, Text: "Ngày Tàn", Syllables: []vnphon.Syllable{syl(t, "ngày"), syl(t, "tàn")}, IsProper: true},
	}
	q = NewQuery([]vnphon.Syllable{syl(t, "ngày"), syl(t, "lan")})
	resp, _ = e.Search(q, mixed)
	var order []string
	for _, r := range resp.Results {
		order = append(order, r.Candidate.Text)
	}
	// …but never ahead of a longer match: a 2-syllable name beats 1-syllable words.
	if strings.Join(order, "|") != "may mắn|Ngày Tàn|tán|Tàn" {
		t.Errorf("order = %v", order)
	}

	// Queries longer than MaxSyl keep the last five syllables and warn.
	long, _ := vnphon.ParsePhrase("một hai ba bốn năm ngày tàn")
	resp, err = e.Search(NewQuery(long), cands)
	if err != nil || len(resp.Warnings) != 1 || len(resp.Syllables) != MaxSyl {
		t.Errorf("long query: %+v, %v", resp, err)
	}
}

func TestSearchDeterministicTies(t *testing.T) {
	e := repoEngine(t)
	mk := func(id int64, text string) Candidate {
		p, _ := vnphon.ParsePhrase(text)
		return Candidate{ID: id, Text: text, Syllables: p}
	}
	cands := []Candidate{mk(3, "lan"), mk(2, "an"), mk(1, "lan")}
	q := NewQuery([]vnphon.Syllable{syl(t, "tan")})
	resp, err := e.Search(q, cands)
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, r := range resp.Results {
		order = append(order, r.Candidate.ID)
	}
	if len(order) != 3 || order[0] != 2 || order[1] != 1 || order[2] != 3 {
		t.Errorf("tie order = %v, want [2 1 3] (text, then id)", order)
	}
}

func TestScore(t *testing.T) {
	if got := Score(2, L0, 0, 0, 2, 2); got != 230 {
		t.Errorf("base = %v", got)
	}
	// Secondary terms are bounded so a longer match always wins.
	worst := Score(2, L3, 0, 0, 5, 1)
	best := Score(1, L0, 100, 100, 1, 1)
	if worst <= best {
		t.Errorf("2-syllable %v must beat 1-syllable %v", worst, best)
	}
	if got := Score(1, L2, -3, -1, 1, 1); got != 118 {
		t.Errorf("negatives clamp to 0: %v", got)
	}
	if got := DecayTrend(1, 90*24*time.Hour); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("decay after 90 days = %v", got)
	}
}

func TestEngineKeys(t *testing.T) {
	e := repoEngine(t)
	p, _ := vnphon.ParsePhrase("tàn")
	if got := e.Keys(Nam, p)[L0]; got != "A.ng.2" {
		t.Errorf("nam L0 tàn = %q", got)
	}
	if e.Keyer() == nil {
		t.Error("Keyer")
	}
}

func TestReadLexicon(t *testing.T) {
	c, err := ReadLexicon(strings.NewReader("# c\n\nngày tàn\t3.5\nchill phết\t\tslang\n"))
	if err == nil {
		t.Fatalf("chill is not Vietnamese, want error, got %+v", c)
	}
	c, err = ReadLexicon(strings.NewReader("# c\n\nngày tàn\t3.5\nnhìu\tslang\n"))
	if err != nil || len(c) != 2 || c[0].Zipf != 3.5 || !c[1].IsSlang || c[1].ID != 2 {
		t.Fatalf("ReadLexicon = %+v, %v", c, err)
	}
	if _, err := ReadLexicon(strings.NewReader("tàn\tabc\n")); err == nil {
		t.Error("bad field must fail")
	}
}

func containsText(r Response, text string) bool {
	for _, x := range r.Results {
		if x.Candidate.Text == text {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWindowKeysAndConfigID(t *testing.T) {
	e := repoEngine(t)
	p, _ := vnphon.ParsePhrase("ngày tàn")
	keys := e.WindowKeys(p)
	// 3 windows (ngày, tàn, ngày tàn) × 5 levels × 3 dialects.
	if len(keys) != 45 {
		t.Fatalf("got %d keys", len(keys))
	}
	found := false
	for _, k := range keys {
		if k.Dialect == Nam && k.Level == L0 && k.Start == 0 && k.N == 2 && k.Key == "AW.j.2 A.ng.2" {
			found = true
		}
	}
	if !found {
		t.Error("missing nam L0 key for the whole phrase")
	}
	if len(e.ConfigID()) != 64 {
		t.Errorf("ConfigID = %q", e.ConfigID())
	}
	if f, d := e.ConfigSource(); !strings.Contains(f, "nucleus_families") || !strings.Contains(d, "nam:") {
		t.Error("ConfigSource must return the YAML")
	}
	if e2 := repoEngine(t); e2.ConfigID() != e.ConfigID() {
		t.Error("same files, same id")
	}
}

// The packed-code comparison used by Search must agree with comparing key
// strings for every pair of (nucleus, coda, tone), in every dialect.
func TestCodeLevelMatchesKeys(t *testing.T) {
	for _, e := range []*Engine{repoEngine(t), mustEngine(t, testFamilies+"l3_coda_mode: families\n")} {
		var all []vnphon.Syllable
		for _, n := range vnphon.Nuclei {
			for _, c := range vnphon.Codas {
				for tone := 1; tone <= 6; tone++ {
					all = append(all, vnphon.Syllable{Nucleus: n, Coda: c, Tone: tone})
				}
			}
		}
		for _, d := range Dialects {
			mapped := e.Map(d, all)
			dm := &e.codes.dialect[dialectIndex(d)]
			for i, a := range all {
				if got, want := dm[e.code(a)], e.code(mapped[i]); got != want {
					t.Fatalf("%s: code map of %+v = %d, want %d", d, a, got, want)
				}
			}
			// Keys and codes once per syllable; then every pair is compared.
			keys := make([][]string, len(mapped))
			codes := make([]Code, len(mapped))
			for i, s := range mapped {
				keys[i] = make([]string, len(Levels))
				for li, l := range Levels {
					keys[i][li] = e.keyer.Key(l, s)
				}
				codes[i] = e.code(s)
			}
			for i := range mapped {
				for j := range mapped {
					a, b := mapped[i], mapped[j]
					want, wantOK := Level(0), false
					for li, l := range Levels {
						if keys[i][li] == keys[j][li] {
							want, wantOK = l, true
							break
						}
					}
					got, ok := e.codes.codeLevel(codes[i], codes[j])
					if ok != wantOK || (ok && got != want) {
						t.Fatalf("%s: %+v vs %+v: code says %v/%v, keys say %v/%v", d, a, b, got, ok, want, wantOK)
					}
				}
			}
		}
	}
	if _, ok := (&codeTables{}).codeLevel(0, 1); ok {
		t.Error("code 0 never rhymes")
	}
	if e := repoEngine(t); e.code(vnphon.Syllable{Nucleus: "X"}) != 0 {
		t.Error("unknown nucleus must give code 0")
	}
}

func mustEngine(t *testing.T, families string) *Engine {
	t.Helper()
	d, err := LoadDialects(strings.NewReader("bac: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(familiesFor(t, families), d)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func familiesFor(t *testing.T, y string) *FamiliesConfig { return families(t, y) }

func enCand(t *testing.T, id int64, text, pron string) Candidate {
	t.Helper()
	segs, err := en.Segments(en.ParsePhones(pron))
	if err != nil {
		t.Fatal(err)
	}
	return Candidate{ID: id, Text: text, Segs: segs}
}

func TestSearchEnglish(t *testing.T) {
	e := repoEngine(t)
	cands := []Candidate{
		enCand(t, 1, "honey", "HH AH1 N IY0"), enCand(t, 2, "tummy", "T AH1 M IY0"),
		enCand(t, 3, "mono", "M OW1 N OW0"), enCand(t, 4, "money", "M AH1 N IY0"),
		enCand(t, 5, "company", "K AH1 M P AH0 N IY0"), enCand(t, 6, "tree", "T R IY1"),
		{ID: 7, Text: "nhà", Syllables: []vnphon.Syllable{syl(t, "nhà")}}, // no segments: skipped
	}
	query := func(l Level) []string {
		q := Query{Lang: entry.EN, Segs: cands[3].Segs, Text: "money", Level: l, MinSyl: 1, MaxSyl: MaxSyl, Match: AlignSuffix, Limit: 50}
		resp, err := e.Search(q, cands)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range resp.Results {
			out = append(out, r.Candidate.Text+"/"+r.Level.String())
		}
		return out
	}
	// money = AH1 N | IY0: the rhyme starts on the stressed AH, so the lone
	// unstressed -y (tree, and the endings of tummy/company) does not count.
	if got := strings.Join(query(Perfect), " "); got != "honey/perfect" {
		t.Errorf("perfect: %s", got)
	}
	if got := strings.Join(query(Family), " "); got != "honey/perfect tummy/family company/family" {
		t.Errorf("family: %s", got)
	}
	if got := strings.Join(query(Assonance), " "); strings.Contains(got, "mono") || strings.Contains(got, "money/") {
		t.Errorf("assonance must exclude mono and the query itself: %s", got)
	}

	bad := []Query{
		{Lang: entry.EN, Level: Family, MinSyl: 1, MaxSyl: 5, Match: AlignSuffix, Limit: 1},                               // no segments
		{Lang: entry.EN, Segs: cands[0].Segs, Level: L25, MinSyl: 1, MaxSyl: 5, Match: AlignSuffix, Limit: 1},             // VI level
		{Lang: entry.EN, Segs: cands[0].Segs, Level: Family, MinSyl: 0, MaxSyl: 5, Match: AlignSuffix, Limit: 1},          // min_syl
		{Lang: entry.EN, Segs: cands[0].Segs, Level: Family, MinSyl: 1, MaxSyl: 5, Match: AlignSuffix, Limit: 0},          // limit
		{Lang: entry.EN, Segs: cands[0].Segs, Level: Family, MinSyl: 1, MaxSyl: 5, Match: "x", Limit: 1},                  // match
		{Syllables: []vnphon.Syllable{syl(t, "tàn")}, Level: Perfect, MinSyl: 1, MaxSyl: 5, Match: AlignSuffix, Limit: 1}, // EN level for VI
	}
	for i, q := range bad {
		if _, err := e.Search(q, cands); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("bad query %d: %v", i, err)
		}
	}
	if ks := WindowKeysEN(cands[4].Segs); len(ks) == 0 || ks[0].Dialect != "" {
		t.Error("English window keys have no dialect")
	}
	if l, err := ParseLevel("Assonance"); err != nil || l != Assonance || !l.IsEnglish() || L3.IsEnglish() {
		t.Error("English levels")
	}
}
