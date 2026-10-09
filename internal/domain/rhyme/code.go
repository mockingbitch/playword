package rhyme

import (
	"slices"

	"playword/pkg/vnphon"
)

// A Code packs the rhyming part of a syllable — nucleus, coda, tone; onset
// and medial never affect rhyme — into one integer, so the hot path of
// Search can map dialects and compare levels with array lookups instead of
// building key strings. Code 0 means "not a valid rhyme".
type Code uint16

const (
	toneBits = 3
	codaBits = 4
	numCodes = 1 << (toneBits + codaBits + 4)
)

func pack(n, c, t int) Code { return Code(n<<(codaBits+toneBits) | c<<toneBits | t) }

func (c Code) nucleus() int { return int(c >> (codaBits + toneBits)) }
func (c Code) coda() int    { return int(c>>toneBits) & (1<<codaBits - 1) }
func (c Code) tone() int    { return int(c) & (1<<toneBits - 1) }

// codeTables are derived from the Keyer and mappers once per Engine.
type codeTables struct {
	nucleusFamily []int             // nucleus index -> family id
	crossClass    []int             // coda index -> L2.5 class id
	l3Class       []int             // coda index -> L3 class id
	dialect       [3][numCodes]Code // dialect index -> code -> mapped code
}

func ids(names []string) []int {
	out := make([]int, len(names))
	seen := map[string]int{}
	for i, n := range names {
		if _, ok := seen[n]; !ok {
			seen[n] = len(seen) + 1
		}
		out[i] = seen[n]
	}
	return out
}

func (e *Engine) buildCodeTables() {
	t := &codeTables{}
	var fam, cross, l3 []string
	for _, n := range vnphon.Nuclei {
		fam = append(fam, e.keyer.nucleusFamily[n])
	}
	for _, c := range vnphon.Codas {
		cross = append(cross, e.keyer.crossClass[c])
		l3 = append(l3, e.keyer.l3Class[c])
	}
	t.nucleusFamily, t.crossClass, t.l3Class = ids(fam), ids(cross), ids(l3)
	for di, d := range Dialects {
		for n := range vnphon.Nuclei {
			for c := range vnphon.Codas {
				for tone := vnphon.ToneNgang; tone <= vnphon.ToneNang; tone++ {
					s := vnphon.Syllable{Nucleus: vnphon.Nuclei[n], Coda: vnphon.Codas[c], Tone: tone}
					t.dialect[di][pack(n, c, tone)] = e.code(e.mappers[d].Map(s))
				}
			}
		}
	}
	e.codes = t
}

func (e *Engine) code(s vnphon.Syllable) Code {
	n := slices.Index(vnphon.Nuclei, s.Nucleus)
	c := slices.Index(vnphon.Codas, s.Coda)
	if n < 0 || c < 0 || s.Tone < vnphon.ToneNgang || s.Tone > vnphon.ToneNang {
		return 0
	}
	return pack(n, c, s.Tone)
}

// Codes packs the syllables of a phrase (standard orthography, unmapped).
// Index builders store this on Candidate.Codes so Search need not redo it.
func (e *Engine) Codes(p []vnphon.Syllable) []Code {
	out := make([]Code, len(p))
	for i, s := range p {
		out[i] = e.code(s)
	}
	return out
}

func dialectIndex(d Dialect) int { return slices.Index(Dialects, d) }

// codeLevel is the strictest level at which two mapped codes rhyme; it
// agrees with comparing Keyer.Key strings (see TestCodeLevelMatchesKeys).
func (t *codeTables) codeLevel(a, b Code) (Level, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	if a.nucleus() == b.nucleus() && a.coda() == b.coda() {
		switch {
		case a.tone() == b.tone():
			return L0, true
		case toneGroup(a.tone()) == toneGroup(b.tone()):
			return L1, true
		}
		return L2, true
	}
	if t.nucleusFamily[a.nucleus()] != t.nucleusFamily[b.nucleus()] {
		return 0, false
	}
	if t.crossClass[a.coda()] == t.crossClass[b.coda()] {
		return L25, true
	}
	if t.l3Class[a.coda()] == t.l3Class[b.coda()] {
		return L3, true
	}
	return 0, false
}
