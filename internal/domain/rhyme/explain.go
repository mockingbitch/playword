package rhyme

import "playword/pkg/vnphon"

// Diff names what differs between two matched syllables: the parts of the
// phonetic distance (closeness.go) a rapper can hear. Zero is a perfect echo
// of the rime.
type Diff uint8

// Diff flags, one per part of the distance.
const (
	DiffTone       Diff = 1 << iota // another tone of the same bằng/trắc group
	DiffToneGroup                   // bằng against trắc
	DiffCoda                        // cross-pair coda (n~ng, w~m)
	DiffCodaFamily                  // coda of the same L3 family
	DiffNucleus                     // vowel of the same family
	DiffMedial                      // medial w present on one side only
)

// Pair is one matched syllable of a result next to the query's.
type Pair struct {
	Query     vnphon.Syllable
	Candidate vnphon.Syllable
	Diff      Diff
	Identical bool // the same word as the query's (spelled the same)
}

// Explain says, syllable by syllable, why cand[start:end] rhymes with the
// end of query in dialect d: what the ear hears as different. It uses the
// same rules as the ranking distance, so the explanation and the order
// agree. It returns nil when the window does not fit.
func (e *Engine) Explain(query []vnphon.Syllable, d Dialect, cand []vnphon.Syllable, start, end int) []Pair {
	n := end - start
	if n <= 0 || start < 0 || end > len(cand) || n > len(query) {
		return nil
	}
	dmap := &e.codes.dialect[dialectIndex(d)]
	qs, cs := query[len(query)-n:], cand[start:end]
	out := make([]Pair, n)
	for i := range n {
		qu, cu := e.code(qs[i]), e.code(cs[i])
		diff := e.codes.sylDiff(dmap[qu], dmap[cu])
		sameMedial := qs[i].Medial == cs[i].Medial
		if !sameMedial {
			diff |= DiffMedial
		}
		out[i] = Pair{Query: qs[i], Candidate: cs[i], Diff: diff,
			Identical: sameMedial && qu == cu && qs[i].Onset == cs[i].Onset}
	}
	return out
}

// sylDiff is sylDistance by part (TestDiffMatchesDistance keeps the two equal).
func (t *codeTables) sylDiff(a, b Code) Diff {
	var d Diff
	switch ta, tb := a.tone(), b.tone(); {
	case ta == tb:
	case (ta <= vnphon.ToneHuyen) == (tb <= vnphon.ToneHuyen):
		d |= DiffTone
	default:
		d |= DiffToneGroup
	}
	switch ca, cb := a.coda(), b.coda(); {
	case ca == cb:
	case t.crossClass[ca] == t.crossClass[cb]:
		d |= DiffCoda
	default:
		d |= DiffCodaFamily
	}
	if a.nucleus() != b.nucleus() {
		d |= DiffNucleus
	}
	return d
}

// Distance is the weight of d in the ranking distance (closeness.go).
func (d Diff) Distance() int {
	w := 0
	for _, p := range [...]struct {
		f Diff
		w int
	}{{DiffTone, distToneGroup}, {DiffToneGroup, distToneOther}, {DiffCoda, distCodaCross},
		{DiffCodaFamily, distCodaL3}, {DiffNucleus, distNucleus}, {DiffMedial, distMedial}} {
		if d&p.f != 0 {
			w += p.w
		}
	}
	return w
}
