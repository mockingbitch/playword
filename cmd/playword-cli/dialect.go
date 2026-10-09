package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"playword/internal/adapters/sources/wiktionary"
	"playword/internal/domain/rhyme"
	"playword/pkg/vnphon"
)

// dialectCheckCmd compares configs/dialects.yaml with the regional IPA of the
// Vietnamese Wiktionary (Saigon → nam, Huế → trung): for every one-syllable
// word, does the mapped coda match the IPA final, and which tone contours
// coincide? Evidence for tuning the YAML; the ear still decides.
func dialectCheckCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("dialect-check", flag.ContinueOnError)
	config := fs.String("config", "configs", "rhyme config directory")
	file := fs.String("file", "", "kaikki.org vi.wiktionary extract (raw-wiktextract-data.jsonl[.gz])")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("-file is required (%s)", wiktionary.DefaultURL)
	}
	e, err := rhyme.LoadDir(*config)
	if err != nil {
		return err
	}
	f, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer f.Close()

	type key struct{ region, rule, predicted, actual string }
	counts, examples := map[key]int{}, map[key][]string{}
	tones := map[string]map[int]map[string]int{}
	regions := map[string]string{"Saigon": "nam", "Huế": "trung", "Hà-Nội": "bac"}
	seen := map[string]bool{}
	err = wiktionary.Each(context.Background(), f, func(rec wiktionary.Record) error {
		if rec.LangCode != "vi" || strings.Contains(rec.Word, " ") || seen[rec.Word] {
			return nil
		}
		s, err := vnphon.ParseSyllable(rec.Word)
		if err != nil {
			return nil
		}
		seen[rec.Word] = true
		for _, snd := range rec.Sounds {
			region := ""
			for _, t := range snd.Tags {
				if r, ok := regions[t]; ok {
					region = r
				}
			}
			if region == "" || snd.IPA == "" || strings.Contains(snd.IPA, " ") {
				continue
			}
			mapped := e.Map(rhyme.Dialect(region), []vnphon.Syllable{s})[0]
			if s.Coda != "" && s.Coda != "j" && s.Coda != "w" {
				k := key{region, s.Nucleus + "+" + s.Coda, mapped.Coda, ipaFinal(snd.IPA)}
				counts[k]++
				if len(examples[k]) < 3 {
					examples[k] = append(examples[k], rec.Word+" ["+snd.IPA+"]")
				}
			}
			if !vnphon.IsStopCoda(s.Coda) {
				if tones[region] == nil {
					tones[region] = map[int]map[string]int{}
				}
				if tones[region][s.Tone] == nil {
					tones[region][s.Tone] = map[string]int{}
				}
				tones[region][s.Tone][ipaTone(snd.IPA)]++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, region := range []string{"nam", "trung"} {
		agree, total := 0, 0
		type row struct {
			k key
			n int
		}
		var bad []row
		for k, n := range counts {
			if k.region != region {
				continue
			}
			total += n
			if k.predicted == k.actual {
				agree += n
			} else {
				bad = append(bad, row{k, n})
			}
		}
		sort.Slice(bad, func(i, j int) bool { return bad[i].n > bad[j].n })
		fmt.Fprintf(out, "%s: coda matches IPA %d/%d (%.1f%%)\n", region, agree, total, 100*float64(agree)/float64(max(total, 1)))
		for _, b := range bad[:min(10, len(bad))] {
			fmt.Fprintf(out, "  %-8s mapped %-3s IPA %-3s ×%-4d %s\n", b.k.rule, b.k.predicted, b.k.actual, b.n, strings.Join(examples[b.k], ", "))
		}
	}
	names := []string{"", "ngang", "huyền", "sắc", "hỏi", "ngã", "nặng"}
	for _, region := range []string{"bac", "nam", "trung"} {
		fmt.Fprintf(out, "%s tones (most common contour):", region)
		for t := 1; t <= 6; t++ {
			best, bn := "", 0
			for c, n := range tones[region][t] {
				if n > bn || (n == bn && c < best) {
					best, bn = c, n
				}
			}
			fmt.Fprintf(out, "  %s %s", names[t], best)
		}
		fmt.Fprintln(out)
	}
	return nil
}

// ipaFinal is the final consonant or glide of one IPA syllable, as a coda code.
func ipaFinal(ipa string) string {
	s := strings.Map(func(r rune) rune {
		if strings.ContainsRune("˥˦˧˨˩ˀʔ", r) || (unicode.Is(unicode.Mn, r) && r != 0x0361) {
			return -1
		}
		return r
	}, ipa)
	for _, f := range []struct{ ipa, code string }{{"ŋ͡m", "ng"}, {"k͡p", "k"}, {"ŋ", "ng"}, {"ɲ", "nh"}, {"n", "n"},
		{"m", "m"}, {"k", "k"}, {"c", "ch"}, {"t", "t"}, {"p", "p"}, {"j", "j"}, {"w", "w"}} {
		if strings.HasSuffix(s, f.ipa) {
			return f.code
		}
	}
	return ""
}

func ipaTone(ipa string) string {
	var b strings.Builder
	for _, r := range ipa {
		if strings.ContainsRune("˥˦˧˨˩ˀʔ", r) || r == 0x0330 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
