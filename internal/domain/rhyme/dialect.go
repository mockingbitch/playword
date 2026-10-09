package rhyme

import (
	"fmt"
	"slices"
	"strings"

	"playword/pkg/vnphon"
)

// Dialect is a regional pronunciation that keys are computed for (spec §1.8).
type Dialect string

// The three dialects.
const (
	Bac   Dialect = "bac"
	Trung Dialect = "trung"
	Nam   Dialect = "nam"
)

// Dialects lists every dialect, in a fixed order.
var Dialects = []Dialect{Bac, Trung, Nam}

// ParseDialect accepts bac, trung or nam in any case.
func ParseDialect(s string) (Dialect, error) {
	d := Dialect(strings.ToLower(s))
	if !slices.Contains(Dialects, d) {
		return "", fmt.Errorf("%w: unknown dialect %q", ErrInvalidQuery, s)
	}
	return d, nil
}

// DialectMapper rewrites a standard-orthography syllable into how it sounds
// in a dialect, before rhyme keys are computed. Map is idempotent.
type DialectMapper interface {
	Map(vnphon.Syllable) vnphon.Syllable
}

type ruleMapper struct {
	nucleus map[[2]string]string // {nucleus, coda} -> nucleus
	coda    map[[2]string]string // {coda, nucleus} -> coda
	tone    [vnphon.ToneNang + 1]int
}

func (m *ruleMapper) Map(s vnphon.Syllable) vnphon.Syllable {
	out := s
	if n, ok := m.nucleus[[2]string{s.Nucleus, s.Coda}]; ok {
		out.Nucleus = n
	}
	if c, ok := m.coda[[2]string{s.Coda, s.Nucleus}]; ok {
		out.Coda = c
	}
	if s.Tone >= 0 && s.Tone < len(m.tone) {
		out.Tone = m.tone[s.Tone]
	}
	return out
}

// NewMapper compiles rules and rejects them unless they are unambiguous and
// idempotent over every rime × tone.
func NewMapper(name string, r DialectRules) (DialectMapper, error) {
	m := &ruleMapper{nucleus: map[[2]string]string{}, coda: map[[2]string]string{}}
	isNucleus := func(c string) bool { return slices.Contains(vnphon.Nuclei, c) }
	isCoda := func(c string) bool { return slices.Contains(vnphon.Codas, c) }

	for _, rule := range r.NucleusRules {
		if !isNucleus(rule.From) || !isNucleus(rule.To) || len(rule.WhenCoda) == 0 {
			return nil, configErr("%s: nucleus rule %+v: needs valid from, to and when_coda", name, rule)
		}
		for _, c := range rule.WhenCoda {
			if !isCoda(c) {
				return nil, configErr("%s: nucleus rule %+v: unknown coda %q", name, rule, c)
			}
			key := [2]string{rule.From, c}
			if _, dup := m.nucleus[key]; dup {
				return nil, configErr("%s: two nucleus rules match %s + coda %q", name, rule.From, c)
			}
			m.nucleus[key] = rule.To
		}
	}

	for _, rule := range r.CodaRules {
		if !isCoda(rule.From) || !isCoda(rule.To) {
			return nil, configErr("%s: coda rule %+v: unknown coda", name, rule)
		}
		for _, n := range slices.Concat(rule.WhenNucleus, rule.UnlessNucleus) {
			if !isNucleus(n) {
				return nil, configErr("%s: coda rule %+v: unknown nucleus %q", name, rule, n)
			}
		}
		for _, n := range vnphon.Nuclei {
			if (len(rule.WhenNucleus) > 0 && !slices.Contains(rule.WhenNucleus, n)) || slices.Contains(rule.UnlessNucleus, n) {
				continue
			}
			key := [2]string{rule.From, n}
			if _, dup := m.coda[key]; dup {
				return nil, configErr("%s: two coda rules match coda %q after %s", name, rule.From, n)
			}
			m.coda[key] = rule.To
		}
	}

	for t := range m.tone {
		m.tone[t] = t
	}
	for from, to := range r.ToneMerge {
		if from < vnphon.ToneNgang || from > vnphon.ToneNang || to < vnphon.ToneNgang || to > vnphon.ToneNang {
			return nil, configErr("%s: tone_merge %d: %d out of range 1..6", name, from, to)
		}
		m.tone[from] = to
	}

	var bad []string
	for _, rm := range vnphon.Rimes() {
		for t := vnphon.ToneNgang; t <= vnphon.ToneNang; t++ {
			s := vnphon.Syllable{Medial: rm.Medial, Nucleus: rm.Nucleus, Coda: rm.Coda, Tone: t}
			once := m.Map(s)
			if twice := m.Map(once); twice != once && len(bad) < 5 {
				bad = append(bad, fmt.Sprintf("%s.%s.%d → %s.%s.%d → %s.%s.%d",
					s.Nucleus, codaCode(s.Coda), s.Tone, once.Nucleus, codaCode(once.Coda), once.Tone,
					twice.Nucleus, codaCode(twice.Coda), twice.Tone))
			}
		}
	}
	if len(bad) > 0 {
		return nil, configErr("%s: rules are not idempotent (Map(Map(s)) != Map(s)): %s", name, strings.Join(bad, "; "))
	}
	return m, nil
}
