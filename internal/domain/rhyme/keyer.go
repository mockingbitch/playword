package rhyme

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"playword/pkg/vnphon"
)

// Level is a rhyme strictness level; lower is stricter and every level
// contains the ones before it (ADR-001 §4).
type Level uint8

// The levels, strictest first.
const (
	L0  Level = iota // nucleus + coda + tone
	L1               // nucleus + coda + tone group (bằng/trắc)
	L2               // nucleus + coda
	L25              // nucleus family + cross-equivalent coda ("rap-flex")
	L3               // nucleus family + coda family (slant)

	// English levels (spec §1.6), strictest first.
	Perfect   // same phonemes from the matched vowel on
	Family    // vowel and consonant groups
	Assonance // vowel groups only
)

// Levels lists the Vietnamese levels from strictest to loosest.
var Levels = []Level{L0, L1, L2, L25, L3}

// ENLevels lists the English levels from strictest to loosest.
var ENLevels = []Level{Perfect, Family, Assonance}

var levelNames = [...]string{L0: "L0", L1: "L1", L2: "L2", L25: "L25", L3: "L3",
	Perfect: "perfect", Family: "family", Assonance: "assonance"}

// IsEnglish reports whether l is one of the English levels.
func (l Level) IsEnglish() bool { return l >= Perfect && l <= Assonance }

func (l Level) String() string {
	if int(l) < len(levelNames) {
		return levelNames[l]
	}
	return "L?" + strconv.Itoa(int(l))
}

// MarshalText encodes a level as its name (L0 … L3).
func (l Level) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// ParseLevel accepts L0, L1, L2, L25 (or L2.5), L3, perfect, family and
// assonance.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "perfect":
		return Perfect, nil
	case "family":
		return Family, nil
	case "assonance":
		return Assonance, nil
	}
	switch strings.ToUpper(s) {
	case "L0":
		return L0, nil
	case "L1":
		return L1, nil
	case "L2":
		return L2, nil
	case "L25", "L2.5":
		return L25, nil
	case "L3":
		return L3, nil
	}
	return 0, fmt.Errorf("%w: unknown level %q", ErrInvalidQuery, s)
}

// Keyer turns a (dialect-mapped) syllable into its rhyme key at each level.
type Keyer struct {
	nucleusFamily map[string]string
	crossClass    map[string]string // coda -> L2.5 class
	l3Class       map[string]string // coda -> L3 class
}

// NewKeyer validates the families config and precomputes coda classes.
func NewKeyer(c *FamiliesConfig) (*Keyer, error) {
	nf, err := partition("nucleus", c.NucleusFamilies, vnphon.Nuclei)
	if err != nil {
		return nil, err
	}
	cf, err := partition("coda", c.CodaFamilies, vnphon.Codas)
	if err != nil {
		return nil, err
	}

	cross := newUnionFind()
	for _, pair := range c.CrossCoda {
		if len(pair) != 2 || !slices.Contains(vnphon.Codas, pair[0]) || !slices.Contains(vnphon.Codas, pair[1]) {
			return nil, configErr("cross_coda_equivalents: %q must be a pair of coda codes", pair)
		}
		cross.union(pair[0], pair[1])
	}
	k := &Keyer{nucleusFamily: nf, crossClass: map[string]string{}, l3Class: map[string]string{}}
	for _, coda := range vnphon.Codas {
		k.crossClass[coda] = className(cross.members(coda, vnphon.Codas), codaCode)
	}

	switch c.L3CodaMode {
	case "", "merged":
		// Families joined by cross pairs, so that L2.5 ⊂ L3.
		merged := newUnionFind()
		for _, coda := range vnphon.Codas {
			merged.union(coda, "family:"+cf[coda]) // prefixed: names must not clash with codes
			merged.union(coda, cross.find(coda))
		}
		for _, coda := range vnphon.Codas {
			var fams []string
			for _, m := range merged.members(coda, vnphon.Codas) {
				if !slices.Contains(fams, cf[m]) {
					fams = append(fams, cf[m])
				}
			}
			slices.Sort(fams)
			k.l3Class[coda] = strings.Join(fams, "+")
		}
	case "families":
		k.l3Class = cf
	default:
		return nil, configErr("l3_coda_mode: %q (want merged or families)", c.L3CodaMode)
	}
	return k, nil
}

// partition checks that families split universe exactly and returns
// code -> family name.
func partition(kind string, families map[string][]string, universe []string) (map[string]string, error) {
	out := map[string]string{}
	for name, codes := range families {
		for _, c := range codes {
			if !slices.Contains(universe, c) {
				return nil, configErr("%s family %s: unknown code %q", kind, name, c)
			}
			if prev, dup := out[c]; dup {
				return nil, configErr("%s code %q is in both %s and %s", kind, c, prev, name)
			}
			out[c] = name
		}
	}
	for _, c := range universe {
		if _, ok := out[c]; !ok {
			return nil, configErr("%s code %q belongs to no family", kind, c)
		}
	}
	return out, nil
}

func codaCode(c string) string {
	if c == "" {
		return "_"
	}
	return c
}

func className(members []string, label func(string) string) string {
	parts := make([]string, len(members))
	for i, m := range members {
		parts[i] = label(m)
	}
	return strings.Join(parts, "+")
}

// Key returns the rhyme key of one syllable at level l.
func (k *Keyer) Key(l Level, s vnphon.Syllable) string {
	switch l {
	case L0:
		return s.Nucleus + "." + codaCode(s.Coda) + "." + strconv.Itoa(s.Tone)
	case L1:
		return s.Nucleus + "." + codaCode(s.Coda) + "." + toneGroup(s.Tone)
	case L2:
		return s.Nucleus + "." + codaCode(s.Coda)
	case L25:
		return k.nucleusFamily[s.Nucleus] + "." + k.crossClass[s.Coda]
	default:
		return k.nucleusFamily[s.Nucleus] + "." + k.l3Class[s.Coda]
	}
}

// PhraseKey joins the keys of every syllable of p.
func (k *Keyer) PhraseKey(l Level, p []vnphon.Syllable) string {
	parts := make([]string, len(p))
	for i, s := range p {
		parts[i] = k.Key(l, s)
	}
	return strings.Join(parts, " ")
}

// CodaClasses lists which codas share a key at L2.5 or L3, for logging the
// effect of transitive cross pairs.
func (k *Keyer) CodaClasses(l Level) map[string][]string {
	src := k.l3Class
	if l == L25 {
		src = k.crossClass
	}
	out := map[string][]string{}
	for _, c := range vnphon.Codas {
		out[src[c]] = append(out[src[c]], codaCode(c))
	}
	return out
}

func toneGroup(t int) string {
	if t == vnphon.ToneNgang || t == vnphon.ToneHuyen {
		return "B" // bằng
	}
	return "T" // trắc
}

type unionFind map[string]string

func newUnionFind() unionFind { return unionFind{} }

func (u unionFind) find(x string) string {
	for {
		p, ok := u[x]
		if !ok || p == x {
			return x
		}
		x = p
	}
}

func (u unionFind) union(a, b string) {
	if ra, rb := u.find(a), u.find(b); ra != rb {
		u[ra] = rb
	}
}

// members returns every element of universe in x's set, in universe order.
func (u unionFind) members(x string, universe []string) []string {
	root := u.find(x)
	var out []string
	for _, e := range universe {
		if u.find(e) == root {
			out = append(out, e)
		}
	}
	return out
}
