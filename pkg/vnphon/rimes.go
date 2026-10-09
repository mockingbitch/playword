package vnphon

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type rime struct{ medial, nucleus, coda string }

// rimeSource is the rime inventory of ADR-001 §2 as groups of four fields
// (spelling without tone marks, medial, nucleus, coda; "-" = empty).
// The first spelling listed for a (medial, nucleus, coda) is canonical and is
// what Render writes.
const rimeSource = `
a - A -      ai - A j     ao - A w     am - A m     an - A n     ang - A ng
anh - A nh   ap - A p     at - A t     ac - A k     ach - A ch
oa w A -     oai w A j    oam w A m    oan w A n    oang w A ng  oanh w A nh
oap w A p    oat w A t    oac w A k    oach w A ch  oao w A w

ay - AW j    au - AW w    ăm - AW m    ăn - AW n    ăng - AW ng  ăp - AW p
ăt - AW t    ăc - AW k
oay w AW j   oăm w AW m   oăn w AW n   oăng w AW ng oăt w AW t   oăc w AW k

ây - AA j    âu - AA w    âm - AA m    ân - AA n    âng - AA ng  âp - AA p
ât - AA t    âc - AA k
uây w AA j   uân w AA n   uâng w AA ng uât w AA t

e - E -      eo - E w     em - E m     en - E n     eng - E ng   ep - E p
et - E t     ec - E k
oe w E -     oeo w E w    oen w E n    oeng w E ng  oet w E t    oem w E m

ê - EE -     êu - EE w    êm - EE m    ên - EE n    ênh - EE nh  êp - EE p
êt - EE t    êch - EE ch
uê w EE -    uên w EE n   uênh w EE nh uêt w EE t   uêch w EE ch

i - I -      y - I -      iu - I w     im - I m     in - I n     inh - I nh
ip - I p     it - I t     ich - I ch
uy w I -     uyu w I w    uynh w I nh  uyp w I p    uyt w I t    uych w I ch

o - O -      oi - O j     om - O m     on - O n     ong - O ng   op - O p
ot - O t     oc - O k     oong - O ng  ooc - O k

ô - OO -     ôi - OO j    ôm - OO m    ôn - OO n    ông - OO ng  ôp - OO p
ôt - OO t    ôc - OO k

ơ - OW -     ơi - OW j    ơm - OW m    ơn - OW n    ơp - OW p    ơt - OW t
uơ w OW -

u - U -      ui - U j     um - U m     un - U n     ung - U ng   up - U p
ut - U t     uc - U k

ư - UW -     ưi - UW j    ưu - UW w    ưm - UW m    ưn - UW n    ưng - UW ng
ưt - UW t    ưc - UW k

ia - IE -    iêu - IE w   iêm - IE m   iên - IE n   iêng - IE ng iêp - IE p
iêt - IE t   iêc - IE k
yêu - IE w   yêm - IE m   yên - IE n   yêt - IE t
uya w IE -   uyên w IE n  uyêt w IE t

ua - UO -    uôi - UO j   uôm - UO m   uôn - UO n   uông - UO ng uôt - UO t
uôc - UO k

ưa - UOW -   ươi - UOW j  ươu - UOW w  ươm - UOW m  ươn - UOW n  ương - UOW ng
ươp - UOW p  ươt - UOW t  ươc - UOW k
`

var (
	rimes     map[string]rime // spelling -> structure
	canonical map[rime]string // structure -> canonical spelling
	rimeList  []rime          // distinct structures, table order
)

// After "qu" the u is the medial, so only these nuclei can follow.
var quNuclei = map[string]bool{A: true, AW: true, AA: true, E: true, EE: true, I: true, IE: true, OW: true, OO: true}

func init() {
	rimes = map[string]rime{}
	canonical = map[rime]string{}
	f := strings.Fields(rimeSource)
	if len(f)%4 != 0 {
		panic("vnphon: malformed rime table")
	}
	dash := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	for i := 0; i < len(f); i += 4 {
		r := rime{dash(f[i+1]), f[i+2], dash(f[i+3])}
		if _, dup := rimes[f[i]]; dup {
			panic(fmt.Sprintf("vnphon: duplicate rime %q", f[i]))
		}
		rimes[f[i]] = r
		if _, ok := canonical[r]; !ok {
			canonical[r] = f[i]
			rimeList = append(rimeList, r)
		}
	}
}

// Rime is the part of a syllable that rhymes.
type Rime struct{ Medial, Nucleus, Coda string }

// Rimes returns every distinct rime structure in the inventory.
func Rimes() []Rime {
	out := make([]Rime, len(rimeList))
	for i, r := range rimeList {
		out[i] = Rime{r.medial, r.nucleus, r.coda}
	}
	return out
}

// RimeSpellings returns every orthographic rime the parser accepts.
func RimeSpellings() []string {
	return slices.Sorted(maps.Keys(rimes))
}
