package views

import (
	"strconv"
	"unicode/utf16"

	"playword/internal/domain/rhyme"
)

// CheckSlots is how many fills the rhyme check has: seven bright fills under
// black ink that full-colour and colour-blind readers tell apart in every
// pair (CVD ΔE ≥ 8.0, normal ≥ 15.8, ink 7–15:1; docs/plans/rhyme-check.md).
const CheckSlots = 7

// CheckStyle is how one group is drawn.
type CheckStyle struct {
	Slot      int // fill, 1…CheckSlots
	Underline int // 0 none; 1 dashed, 2 dotted: only when a stanza has more groups than fills
}

// CheckStyles gives every group a fill: groups of one stanza never share a
// fill, and each stanza takes the fills least recently used, so
// neighbouring stanzas differ too whenever there are fills enough.
func CheckStyles(r rhyme.CheckResult) []CheckStyle {
	out := make([]CheckStyle, len(r.Groups))
	last := make([]int, CheckSlots+1) // slot -> when it was last used (0: never)
	clock := 0
	for i := 0; i < len(r.Groups); {
		j := i
		for j < len(r.Groups) && r.Groups[j].Stanza == r.Groups[i].Stanza {
			j++
		}
		used := map[int]int{}
		for k := i; k < j; k++ {
			best := 0
			for round := 0; best == 0; round++ {
				for s := 1; s <= CheckSlots; s++ {
					if used[s] == round && (best == 0 || last[s] < last[best]) {
						best = s
					}
				}
			}
			clock++
			last[best] = clock
			out[k] = CheckStyle{Slot: best, Underline: min(used[best], 2)}
			used[best]++
		}
		i = j
	}
	return out
}

// checkSeg is a run of one line: plain text, or a word of a group.
type checkSeg struct {
	Text   string
	Group  int // index into Result.Groups, 1-based
	Repeat bool
	Label  string // the group's letter, shown on the run
}

type checkLine struct {
	Segs   []checkSeg
	Scheme string
	Blank  bool
}

// checkLines cuts the text at the coloured words, line by line.
func checkLines(text string, r rhyme.CheckResult) []checkLine {
	u := utf16.Encode([]rune(text))
	index := map[int]int{}
	for i, g := range r.Groups {
		index[g.ID] = i + 1
	}
	out := make([]checkLine, 0, len(r.Lines))
	pos := 0
	for _, ln := range r.Lines {
		end := pos
		for end < len(u) && u[end] != '\n' {
			end++
		}
		cl := checkLine{Scheme: ln.Scheme, Blank: ln.Stanza < 0}
		at := pos
		for _, t := range r.Tokens[ln.From:ln.To] {
			if t.Group == 0 && !t.Repeat {
				continue
			}
			g := index[t.Group]
			if n := len(cl.Segs); n > 0 && g > 0 && cl.Segs[n-1].Group == g && blankUTF16(u[at:t.Start]) {
				// the same chain goes on: colour the space between its words
				cl.Segs[n-1].Text += string(utf16.Decode(u[at:t.End]))
				at = t.End
				continue
			}
			if t.Start > at {
				cl.Segs = append(cl.Segs, checkSeg{Text: string(utf16.Decode(u[at:t.Start]))})
			}
			seg := checkSeg{Text: string(utf16.Decode(u[t.Start:t.End])), Group: g, Repeat: t.Repeat}
			if g > 0 {
				seg.Label = r.Groups[g-1].Letter
			}
			cl.Segs = append(cl.Segs, seg)
			at = t.End
		}
		if end > at {
			cl.Segs = append(cl.Segs, checkSeg{Text: string(utf16.Decode(u[at:end]))})
		}
		out = append(out, cl)
		pos = end + 1

	}
	return out
}

// checkClass styles a group from its CheckStyle.
func checkClass(st CheckStyle) string {
	return "rg rg" + strconv.Itoa(st.Slot) + " ru" + strconv.Itoa(st.Underline)
}

func groupClass(styles []CheckStyle, group int) string {
	if group <= 0 || group > len(styles) {
		return ""
	}
	return checkClass(styles[group-1])
}

func density(x float64) string {
	s := strconv.FormatFloat(x, 'f', 1, 64)
	for i := range s {
		if s[i] == '.' {
			return s[:i] + "," + s[i+1:]
		}
	}
	return s
}

func joinMembers(ms []string) string {
	out := ""
	for i, m := range ms {
		if i > 0 {
			out += " · "
		}
		if i == 4 && len(ms) > 5 {
			return out + "…"
		}
		out += m
	}
	return out
}

func joinSchemes(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " / "
		}
		out += s
	}
	return out
}

func blankUTF16(u []uint16) bool {
	for _, c := range u {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}
