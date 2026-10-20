package rhyme

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"

	"playword/pkg/vnphon"
)

// Rhyme check: colour the multi-syllable rhymes of a text, the way rappers
// mark a rhyme scheme on paper (.docs/plans/rhyme-check.md).
//
// A chain is two or more consecutive syllables of one line that rhyme,
// syllable by syllable, with as many consecutive syllables elsewhere in the
// same stanza ("ngày tàn" ~ "chạy án"). Chains are grouped strictest match
// first: at each level, from L0 to the requested one, chains not yet in a
// group join the one existing group they touch at that level, or form new
// groups among themselves; existing groups never merge. So "hải quân" ~
// "hai tuần" and "ngày tàn" ~ "chạy án" stay two colours even though the two
// pairs also rhyme with each other at rap-flex.

// DefaultStopwords are function words that rhyme by accident too often to
// carry a chain on their own.
var DefaultStopwords = map[string]bool{
	"là": true, "và": true, "mà": true, "thì": true, "của": true, "cho": true, "với": true, "những": true,
	"các": true, "một": true, "này": true, "đó": true, "đã": true, "sẽ": true, "đang": true, "cũng": true,
	"rồi": true, "nhé": true, "à": true, "ơi": true,
}

// CheckOptions say how to read the text.
type CheckOptions struct {
	Dialect   Dialect
	Level     Level           // loosest level that counts (L0…L3)
	Stopwords map[string]bool // nil: DefaultStopwords
}

// CheckToken is one word of the text. Offsets count UTF-16 code units, the
// way browsers index strings.
type CheckToken struct {
	Text       string
	Start, End int
	Line       int  // index into CheckResult.Lines
	Group      int  // 1-based group, 0 when not in a coloured chain
	Repeat     bool // the same word or phrase said again, not a rhyme
	Syllable   bool // a Vietnamese syllable (others break chains)
	Stop       bool
	code       Code   // mapped to the dialect; 0 when not a syllable
	norm       string // lower case NFC, for comparing words
	word       int    // same number for the same norm
}

// CheckLine is one line of the text.
type CheckLine struct {
	Stanza     int    // 0-based; -1 for a blank line
	From, To   int    // its tokens are Tokens[From:To]
	Scheme     string // letter of the group its last syllable is in, "" if none
	Repeat     bool   // its last word repeats another line's last word
	LongestRun int    // longest rhyme with the two lines before (rhyme density)
}

// CheckGroup is one colour.
type CheckGroup struct {
	ID      int
	Letter  string
	Level   Level    // the level its chains matched at
	Label   string   // the rimes of its first chain, "ai-uân"
	Members []string // the distinct chains, in order of appearance
	Chains  int
	Stanza  int // groups never cross a blank line
}

// CheckResult is the analysis of a whole text.
type CheckResult struct {
	Tokens  []CheckToken
	Lines   []CheckLine
	Groups  []CheckGroup
	Density float64 // mean longest rhyme run per line (Malmi et al. 2016)
	Rhymed  float64 // share of syllables in a coloured chain
}

// Schemes is the rhyme scheme per stanza, "AABB", with "·" for a line
// whose end does not rhyme.
func (r CheckResult) Schemes() []string {
	var out []string
	cur := -1
	for _, l := range r.Lines {
		if l.Stanza < 0 {
			continue
		}
		if l.Stanza != cur {
			out = append(out, "")
			cur = l.Stanza
		}
		s := l.Scheme
		if s == "" {
			s = "·"
		}
		out[len(out)-1] += s
	}
	return out
}

type span struct{ line, from, to int } // token indices into the line, [from, to)

type chainEdge struct {
	a, b  int // node indices
	level Level
}

// Check analyses text. It never fails: words it cannot read are left plain.
func (e *Engine) Check(text string, o CheckOptions) CheckResult {
	if o.Stopwords == nil {
		o.Stopwords = DefaultStopwords
	}
	if o.Level > L3 {
		o.Level = L3
	}
	dmap := &e.codes.dialect[max(dialectIndex(o.Dialect), 0)]
	r := tokenize(text)
	type word struct {
		norm     string
		syllable bool
		code     Code
		stop     bool
		id       int
	}
	words := map[string]word{} // lyrics repeat words a lot: read each once
	ids := map[string]int{}
	for i := range r.Tokens {
		t := &r.Tokens[i]
		w, ok := words[t.Text]
		if !ok {
			w.norm = norm.NFC.String(strings.ToLower(t.Text))
			w.stop = o.Stopwords[w.norm]
			if s, err := vnphon.ParseSyllable(t.Text); err == nil {
				w.syllable = true
				if c := e.code(s); c != 0 {
					w.code = dmap[c]
				}
			}
			if w.id = ids[w.norm]; w.id == 0 { // "Ngày" and "ngày" are one word
				w.id = len(ids) + 1
				ids[w.norm] = w.id
			}
			words[t.Text] = w
		}
		t.norm, t.Stop, t.Syllable, t.code, t.word = w.norm, w.stop, w.syllable, w.code, w.id
	}

	var stanzas [][]int // line indices
	for i, l := range r.Lines {
		if l.Stanza < 0 {
			continue
		}
		for len(stanzas) <= l.Stanza {
			stanzas = append(stanzas, nil)
		}
		stanzas[l.Stanza] = append(stanzas[l.Stanza], i)
	}
	nextID := 0
	for _, lines := range stanzas {
		e.checkStanza(&r, lines, o, &nextID)
	}

	syllables, rhymed, runs, measured := 0, 0, 0, 0
	for i := range r.Tokens {
		if r.Tokens[i].Syllable {
			syllables++
			if r.Tokens[i].Group > 0 {
				rhymed++
			}
		}
	}
	for _, lines := range stanzas {
		for k, li := range lines {
			if k == 0 {
				continue
			}
			best := 0
			for _, pj := range lines[max(0, k-2):k] {
				best = max(best, e.longestRun(&r, pj, li, o.Level))
			}
			r.Lines[li].LongestRun = best
			runs += best
			measured++
		}
	}
	if syllables > 0 {
		r.Rhymed = float64(rhymed) / float64(syllables)
	}
	if measured > 0 {
		r.Density = float64(runs) / float64(measured)
	}
	return r
}

func (e *Engine) lineTokens(r *CheckResult, li int) []CheckToken {
	return r.Tokens[r.Lines[li].From:r.Lines[li].To]
}

// pairLevel is how two words rhyme: not at all when they are the same word
// (a repeat is not a rhyme) or when either is not a syllable.
func (e *Engine) pairLevel(a, b *CheckToken, limit Level) (Level, bool) {
	if a.code == 0 || b.code == 0 || a.word == b.word {
		return 0, false
	}
	l, ok := e.codes.codeLevel(a.code, b.code)
	if !ok || l > limit {
		return 0, false
	}
	return l, true
}

// longestRun is the longest run of rhyming words between two lines, single
// words included (the rhyme density measure).
func (e *Engine) longestRun(r *CheckResult, la, lb int, limit Level) int {
	a, b := e.lineTokens(r, la), e.lineTokens(r, lb)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	best := 0
	for i := range a {
		for j := range b {
			cur[j+1] = 0
			if _, ok := e.pairLevel(&a[i], &b[j], limit); ok {
				cur[j+1] = prev[j] + 1
				best = max(best, cur[j+1])
			}
		}
		prev, cur = cur, prev
	}
	return best
}

func (e *Engine) checkStanza(r *CheckResult, lines []int, o CheckOptions, nextID *int) {
	var nodes []span
	index := map[span]int{}
	node := func(s span) int {
		if i, ok := index[s]; ok {
			return i
		}
		index[s] = len(nodes)
		nodes = append(nodes, s)
		return len(nodes) - 1
	}
	content := func(toks []CheckToken) bool {
		return slices.ContainsFunc(toks, func(t CheckToken) bool { return !t.Stop })
	}
	var edges []chainEdge
	var runBuf []int
	var lvlBuf []Level
	for x, la := range lines {
		for _, lb := range lines[x:] {
			a, b := e.lineTokens(r, la), e.lineTokens(r, lb)
			// run[i*w+j]: length of the rhyming run ending at a[i], b[j]
			w := len(b)
			if cap(runBuf) < len(a)*w {
				runBuf, lvlBuf = make([]int, len(a)*w), make([]Level, len(a)*w)
			}
			run, lvl := runBuf[:len(a)*w], lvlBuf[:len(a)*w]
			clear(run)
			for i := range a {
				for j := range b {
					l, ok := e.pairLevel(&a[i], &b[j], o.Level)
					if !ok {
						continue
					}
					run[i*w+j], lvl[i*w+j] = 1, l
					if i > 0 && j > 0 && run[(i-1)*w+j-1] > 0 {
						run[i*w+j] = run[(i-1)*w+j-1] + 1
						lvl[i*w+j] = max(l, lvl[(i-1)*w+j-1])
					}
				}
			}
			for i := range a {
				for j := range b {
					n := run[i*w+j]
					if n < 2 || (i+1 < len(a) && j+1 < len(b) && run[(i+1)*w+j+1] > 0) {
						continue // too short, or not maximal
					}
					if la == lb && j-n+1 <= i {
						continue // within a line, the two chains must not overlap (and count once)
					}
					sa, sb := span{la, i - n + 1, i + 1}, span{lb, j - n + 1, j + 1}
					if !content(a[sa.from:sa.to]) || !content(b[sb.from:sb.to]) {
						continue
					}
					edges = append(edges, chainEdge{node(sa), node(sb), lvl[i*w+j]})
				}
			}
		}
	}

	// group, strictest level first
	group := make([]int, len(nodes)) // 0: none
	birth := map[int]Level{}
	for _, level := range Levels {
		if level > o.Level {
			break
		}
		var at []chainEdge
		for _, ed := range edges {
			if ed.level == level {
				at = append(at, ed)
			}
		}
		if len(at) == 0 {
			continue
		}
		// components of the ungrouped nodes joined at this level
		uf := map[int]int{}
		var find func(int) int
		find = func(x int) int {
			p, ok := uf[x]
			if !ok || p == x {
				return x
			}
			uf[x] = find(p)
			return uf[x]
		}
		for _, ed := range at {
			if group[ed.a] == 0 && group[ed.b] == 0 {
				uf[find(ed.a)] = find(ed.b)
			}
		}
		comps := map[int][]int{}
		for _, ed := range at {
			for _, n := range []int{ed.a, ed.b} {
				if group[n] == 0 && !slices.Contains(comps[find(n)], n) {
					comps[find(n)] = append(comps[find(n)], n)
				}
			}
		}
		roots := make([]int, 0, len(comps))
		for root := range comps {
			roots = append(roots, root)
		}
		slices.SortFunc(roots, func(x, y int) int { return cmp.Compare(slices.Min(comps[x]), slices.Min(comps[y])) })
		for _, root := range roots {
			members := comps[root]
			// the existing groups this component touches: number of links, nearest line
			type cand struct{ links, dist int }
			cands := map[int]*cand{}
			for _, ed := range at {
				for _, pair := range [][2]int{{ed.a, ed.b}, {ed.b, ed.a}} {
					u, v := pair[0], pair[1]
					// every group v can be in was born at a stricter level: the nodes
					// joined at this level are all in components, not yet in groups
					if group[u] != 0 || group[v] == 0 || find(u) != root {
						continue
					}
					c := cands[group[v]]
					if c == nil {
						c = &cand{dist: 1 << 30}
						cands[group[v]] = c
					}
					c.links++
					c.dist = min(c.dist, abs(nodes[u].line-nodes[v].line))
				}
			}
			target := 0
			for g, c := range cands {
				if t := cands[target]; target == 0 || c.links > t.links || c.links == t.links && (c.dist < t.dist || c.dist == t.dist && g < target) {
					target = g
				}
			}
			if target == 0 {
				if len(members) < 2 {
					continue
				}
				*nextID++
				target = *nextID
				birth[target] = level
			}
			for _, n := range members {
				group[n] = target
			}
		}
	}

	// colour words: see betterClaim
	type claim struct{ node, length int }
	claims := map[[2]int]claim{} // (line, token) -> chain
	for n, s := range nodes {
		g := group[n]
		if g == 0 {
			continue
		}
		for t := s.from; t < s.to; t++ {
			k := [2]int{s.line, t}
			old, ok := claims[k]
			if !ok || betterClaim(s.to-s.from, birth[g], g, old.length, birth[group[old.node]], group[old.node]) {
				claims[k] = claim{n, s.to - s.from}
			}
		}
	}
	// a chain that lost a word to another keeps no colour at all (no
	// one-syllable fragments), and a group needs two chains left whole
	whole := make([]bool, len(nodes))
	intact := map[int]int{}
	for n, s := range nodes {
		if group[n] == 0 {
			continue
		}
		whole[n] = true
		for t := s.from; t < s.to; t++ {
			if claims[[2]int{s.line, t}].node != n {
				whole[n] = false
			}
		}
		if whole[n] {
			intact[group[n]]++
		}
	}
	for k, c := range claims {
		if g := group[c.node]; whole[c.node] && intact[g] >= 2 {
			r.Tokens[r.Lines[k[0]].From+k[1]].Group = g
		}
	}
	e.addGroups(r, nodes, group, birth, intact, whole)
	e.markRepeats(r, lines)
	for _, li := range lines {
		toks := e.lineTokens(r, li)
		for i := len(toks) - 1; i >= 0; i-- {
			if toks[i].Syllable || toks[i].Group > 0 {
				if g := toks[i].Group; g > 0 {
					r.Lines[li].Scheme = r.groupByID(g).Letter
				}
				break
			}
		}
	}
}

// betterClaim decides which chain colours a word two chains share: the
// stricter group first (the user's rule: strictest partner wins), then the
// longer chain, then the earlier group.
func betterClaim(len1 int, lvl1 Level, g1, len2 int, lvl2 Level, g2 int) bool {
	if lvl1 != lvl2 {
		return lvl1 < lvl2
	}
	if len1 != len2 {
		return len1 > len2
	}
	return g1 < g2
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (r *CheckResult) groupByID(id int) *CheckGroup {
	for i := range r.Groups {
		if r.Groups[i].ID == id {
			return &r.Groups[i]
		}
	}
	return nil
}

// addGroups names the stanza's surviving groups in order of appearance.
func (e *Engine) addGroups(r *CheckResult, nodes []span, group []int, birth map[int]Level, intact map[int]int, whole []bool) {
	order := make([]int, len(nodes))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(x, y int) int {
		a, b := nodes[x], nodes[y]
		return cmp.Or(cmp.Compare(a.line, b.line), cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to))
	})
	for _, n := range order {
		g := group[n]
		if g == 0 || intact[g] < 2 || !whole[n] {
			continue
		}
		s := nodes[n]
		toks := r.Tokens[r.Lines[s.line].From+s.from : r.Lines[s.line].From+s.to]
		words := make([]string, len(toks))
		for i, t := range toks {
			words[i] = t.Text
		}
		text := strings.Join(words, " ")
		cg := r.groupByID(g)
		if cg == nil {
			rimes := make([]string, len(toks)) // named after its first chain
			for i, t := range toks {
				rimes[i], _ = vnphon.WrittenRime(t.Text)
			}
			r.Groups = append(r.Groups, CheckGroup{ID: g, Letter: letter(len(r.Groups)), Level: birth[g], Label: strings.Join(rimes, "-"),
				Stanza: r.Lines[s.line].Stanza})
			cg = &r.Groups[len(r.Groups)-1]
		}
		cg.Chains++
		if !slices.ContainsFunc(cg.Members, func(m string) bool { return strings.EqualFold(m, text) }) {
			cg.Members = append(cg.Members, text)
		}
	}
}

// letter names the n-th group: A…Z, then A2, B2…
func letter(n int) string {
	s := string(rune('A' + n%26))
	if n >= 26 {
		s += strconv.Itoa(n/26 + 1)
	}
	return s
}

// markRepeats flags what is said again instead of rhymed: a line ending on
// the same word as another line of the stanza, and identical phrases of two
// words or more.
func (e *Engine) markRepeats(r *CheckResult, lines []int) {
	last := map[int][]int{} // word -> lines ending on it
	for _, li := range lines {
		toks := e.lineTokens(r, li)
		if len(toks) > 0 && !toks[len(toks)-1].Stop {
			w := toks[len(toks)-1].word
			last[w] = append(last[w], li)
		}
	}
	for _, ls := range last {
		if len(ls) < 2 {
			continue
		}
		for _, li := range ls {
			r.Lines[li].Repeat = true
			t := &r.Tokens[r.Lines[li].To-1]
			if t.Group == 0 {
				t.Repeat = true
			}
		}
	}
	for x, la := range lines {
		for _, lb := range lines[x+1:] {
			a, b := e.lineTokens(r, la), e.lineTokens(r, lb)
			for i := range a {
				for j := range b {
					n := 0
					for i+n < len(a) && j+n < len(b) && a[i+n].word == b[j+n].word {
						n++
					}
					if n < 2 || !slices.ContainsFunc(a[i:i+n], func(t CheckToken) bool { return !t.Stop }) {
						continue
					}
					for k := range n {
						for _, t := range []*CheckToken{&r.Tokens[r.Lines[la].From+i+k], &r.Tokens[r.Lines[lb].From+j+k]} {
							if t.Group == 0 {
								t.Repeat = true
							}
						}
					}
				}
			}
		}
	}
}

// tokenize splits text into lines, stanzas and words. Words are runs of
// letters and combining marks; hyphens, digits and punctuation separate them.
func tokenize(text string) CheckResult {
	var r CheckResult
	stanza, blank := 0, true
	off := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		l := CheckLine{From: len(r.Tokens)}
		start, wstart := -1, 0
		pos := off
		flush := func(end int) {
			if start >= 0 {
				w := line[wstart:end]
				r.Tokens = append(r.Tokens, CheckToken{Text: w, Start: start, End: start + utf16Len(w), Line: len(r.Lines)})
				start = -1
			}
		}
		for i, c := range line {
			if unicode.IsLetter(c) || unicode.Is(unicode.Mn, c) {
				if start < 0 {
					start, wstart = pos, i
				}
			} else {
				flush(i)
			}
			pos += utf16.RuneLen(c)
		}
		flush(len(line))
		l.To = len(r.Tokens)
		if strings.TrimSpace(line) == "" {
			l.Stanza = -1
			if !blank {
				stanza++
			}
			blank = true
		} else {
			l.Stanza = stanza
			blank = false
		}
		r.Lines = append(r.Lines, l)
		off += utf16Len(raw) + 1 // the newline
	}
	// stanza numbers without gaps
	seen, next := map[int]int{}, 0
	for i := range r.Lines {
		if s := r.Lines[i].Stanza; s >= 0 {
			if _, ok := seen[s]; !ok {
				seen[s] = next
				next++
			}
			r.Lines[i].Stanza = seen[s]
		}
	}
	return r
}

func utf16Len(s string) int {
	n := 0
	for _, c := range s {
		n += utf16.RuneLen(c)
	}
	return n
}
