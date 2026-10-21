package rhyme

import (
	"playword/internal/domain/rhyme/en"
	"playword/pkg/vnphon"
)

// Closeness orders results inside a level: how far each matched syllable is
// from the query's, so the nearest rhymes come first and word frequency
// only breaks ties (.docs/plans/rhyme-ranking.md, decided 2026-10-21).
//
// Per Vietnamese syllable, on dialect-mapped codes:
//
//	tone      same 0 · same bằng/trắc group 1 · other group 2
//	coda      same 0 · cross pair (n~ng, w~m) 3 · same L3 family 5
//	nucleus   same 0 · same family 4   (a vowel change is heard more than a coda change)
//	medial    same 0 · different 1     (toàn ~ tàn: the keys ignore it, the ear does not)
//
// The onset never counts (no alliteration bonus). The distance of a match is
// the sum over its syllables; it is compared only between results of the
// same level, so a level never overtakes a stricter one.
const (
	distToneGroup = 1
	distToneOther = 2
	distCodaCross = 3
	distCodaL3    = 5
	distNucleus   = 4
	distMedial    = 1
)

// sylDistance is the distance of two dialect-mapped syllable codes that
// rhyme (codeLevel ok).
func (t *codeTables) sylDistance(a, b Code) int {
	d := 0
	switch ta, tb := a.tone(), b.tone(); {
	case ta == tb:
	case (ta <= vnphon.ToneHuyen) == (tb <= vnphon.ToneHuyen):
		d += distToneGroup
	default:
		d += distToneOther
	}
	switch ca, cb := a.coda(), b.coda(); {
	case ca == cb:
	case t.crossClass[ca] == t.crossClass[cb]:
		d += distCodaCross
	default:
		d += distCodaL3
	}
	if a.nucleus() != b.nucleus() {
		d += distNucleus
	}
	return d
}

// closeness scores one aligned window: the summed distance, and how many of
// its syllables are the query's own word (spelled the same), which rank
// after every other rhyme of the same length (decided 2026-10-21, Q14).
// qm/cm are dialect-mapped codes, qu/cu the unmapped ones, qs/cs the
// syllables (for medial and onset). Identity compares integer codes first and
// strings only when those agree, which is rare: this runs for every match.
func (e *Engine) closeness(qm, cm, qu, cu []Code, qs, cs []vnphon.Syllable) (dist, identical int) {
	for i := range qm {
		dist += e.codes.sylDistance(qm[i], cm[i])
		if i >= len(qs) || i >= len(cs) {
			continue
		}
		sameMedial := qs[i].Medial == cs[i].Medial
		if !sameMedial {
			dist += distMedial
		}
		if sameMedial && qu[i] == cu[i] && qs[i].Onset == cs[i].Onset {
			identical++
		}
	}
	return dist, identical
}

// English, per segment: vowel same 0 · same group 3; stress (stressed or
// not) differs 3 — an unstressed echo is a weak rhyme to an English ear
// ("money" ~ "tummy" beats "money" ~ "company"); each consonant after the
// vowel same 0 · same group 2 · other 3; each consonant one side lacks 3.
const (
	distENVowel     = 3
	distENStress    = 3
	distENConsGroup = 2
	distENConsOther = 3
)

func segDistance(a, b en.Seg) int {
	d := 0
	if a.Vowel != b.Vowel {
		d += distENVowel
	}
	if (a.Stress > 0) != (b.Stress > 0) {
		d += distENStress
	}
	n := max(len(a.Tail), len(b.Tail))
	for i := range n {
		switch {
		case i >= len(a.Tail) || i >= len(b.Tail):
			d += distENConsOther
		case a.Tail[i] == b.Tail[i]:
		case en.SameConsonantGroup(a.Tail[i], b.Tail[i]):
			d += distENConsGroup
		default:
			d += distENConsOther
		}
	}
	return d
}
