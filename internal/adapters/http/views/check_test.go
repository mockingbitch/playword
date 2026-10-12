package views

import (
	"testing"

	"playword/internal/domain/rhyme"
)

func groups(stanzas ...int) rhyme.CheckResult {
	var r rhyme.CheckResult
	for i, s := range stanzas {
		r.Groups = append(r.Groups, rhyme.CheckGroup{ID: i + 1, Stanza: s})
	}
	return r
}

func TestCheckStyles(t *testing.T) {
	// one stanza: every group its own fill, no underline
	st := CheckStyles(groups(0, 0, 0, 0, 0, 0, 0))
	seen := map[int]bool{}
	for _, s := range st {
		if seen[s.Slot] || s.Underline != 0 {
			t.Fatalf("seven groups in a stanza: %+v", st)
		}
		seen[s.Slot] = true
	}
	// neighbouring stanzas do not share fills when seven are enough
	st = CheckStyles(groups(0, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3))
	byStanza := map[int][]int{}
	for i, s := range st {
		stz := []int{0, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3}[i]
		byStanza[stz] = append(byStanza[stz], s.Slot)
	}
	for stz := 1; stz <= 3; stz++ {
		for _, a := range byStanza[stz] {
			for _, b := range byStanza[stz-1] {
				if a == b {
					t.Errorf("stanzas %d and %d share fill %d: %v", stz-1, stz, a, byStanza)
				}
			}
		}
	}
	// more groups than fills in one stanza: the extra ones get an underline
	st = CheckStyles(groups(0, 0, 0, 0, 0, 0, 0, 0, 0))
	if st[7].Underline != 1 || st[8].Underline != 1 || st[7].Slot == st[8].Slot {
		t.Errorf("overflow: %+v", st[7:])
	}
}
