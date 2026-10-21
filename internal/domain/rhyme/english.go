package rhyme

import (
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme/en"
)

// enLevel maps the shared Level to the en package's.
func enLevel(l Level) en.Level { return en.Level(l - Perfect) }

func fromEN(l en.Level) Level { return Perfect + Level(l) }

// searchEN is SearchIn for English: the same windows, alignment, scoring and
// ordering as Vietnamese, over rhyme segments instead of syllables.
func searchEN(q Query, cands []Candidate, subset []int32) Response {
	var resp Response
	used := q.Segs
	if len(used) > MaxSyl {
		used = used[len(used)-MaxSyl:]
	}
	n := len(cands)
	if subset != nil {
		n = len(subset)
	}
	top := newTopK(q.Limit)
	for k := range n {
		i := k
		if subset != nil {
			i = int(subset[k])
		}
		c := &cands[i]
		if len(c.Segs) == 0 || (q.ExcludeSlang && c.IsSlang) || (q.ExcludeProper && c.IsProper) ||
			(q.Text != "" && entry.Normalize(c.Text) == q.Text) {
			continue
		}
		if r, ok := matchEN(used, c.Segs, q); ok {
			r.Candidate = c
			r.Score = Score(r.Matched, r.Level, c.Zipf, c.Trend, len(c.Segs), len(used))
			top.push(r)
		}
	}
	resp.Results = top.sorted()
	return resp
}

// matchEN tries suffixes of the query that start on a stressed vowel (spec
// §1.6: an English rhyme runs from a stressed vowel to the end), longest
// first. A query with no stressed vowel ("the") may start anywhere.
func matchEN(qs, cs []en.Seg, q Query) (Result, bool) {
	anyStress := false
	for _, s := range qs {
		anyStress = anyStress || s.Stress > 0
	}
	for n := min(len(qs), q.MaxSyl, len(cs)); n >= q.MinSyl; n-- {
		suffix := qs[len(qs)-n:]
		if anyStress && suffix[0].Stress == 0 {
			continue
		}
		best, found := Result{}, false
		lo, hi := windowRange(len(cs), n, q.Match)
		for start := hi; start >= lo; start-- {
			worst, ok, dist := en.Perfect, true, 0
			for i := range suffix {
				l, segOK := en.SegLevel(suffix[i], cs[start+i])
				if !segOK {
					ok = false
					break
				}
				worst = max(worst, l)
				dist += segDistance(suffix[i], cs[start+i])
			}
			lvl := fromEN(worst)
			if !ok || lvl > q.Level {
				continue
			}
			r := Result{Matched: n, Level: lvl, Start: start, End: start + n, Distance: uint16(dist)}
			if !found || closer(r, best) {
				best, found = r, true
			}
		}
		if found {
			return best, true
		}
	}
	return Result{}, false
}

// WindowKeysEN is WindowKeys for English segments: every window of up to
// MaxSyl segments at every English level; Dialect is empty.
func WindowKeysEN(segs []en.Seg) []WindowKey {
	var out []WindowKey
	for start := range segs {
		for n := 1; n <= MaxSyl && start+n <= len(segs); n++ {
			for _, l := range ENLevels {
				out = append(out, WindowKey{Level: l, Start: start, N: n, Key: en.PhraseKey(enLevel(l), segs[start:start+n])})
			}
		}
	}
	return out
}

// PhraseKeyEN is the key of segs at an English level.
func PhraseKeyEN(l Level, segs []en.Seg) string { return en.PhraseKey(enLevel(l), segs) }
