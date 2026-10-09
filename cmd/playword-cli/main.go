// Command playword-cli holds offline tools: parse shows how a phrase is split
// into syllables and keyed; rhymes searches a fixture lexicon so rhyme rules
// can be tried by ear before real data is ingested.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"playword/internal/domain/rhyme"
	"playword/pkg/vnphon"
)

const usage = `usage:
  playword-cli parse        [-config dir] <text>
  playword-cli rhymes       [-config dir] [-lexicon file] [search flags] <text>   (fixture lexicon, no DB)
  playword-cli migrate      [-db dsn] [up|down|status|...]
  playword-cli ingest       [-db dsn] [-config dir] [-file dump] [-dry-run] <source>
  playword-cli rebuild-keys [-db dsn] [-config dir]
  playword-cli user-role    [-db dsn] <email> <user|admin>
  playword-cli user-delete  [-db dsn] -confirm <email>                             (support: erase an account, anonymize its events)
  playword-cli dialect-check [-config dir] -file wiktionary.jsonl.gz              (dialects.yaml vs regional IPA)
  playword-cli healthcheck  <url>                                                  (exit 0 on HTTP 200; for distroless images)
  playword-cli maintain     [-db dsn]                                              (event partitions + retention, views, sessions)
  playword-cli freq         [-db dsn] [-lang vi|en] (-corpus text | -list "word count" file) [-name n -license l -url u] [-dry-run]
  playword-cli search       [-db dsn] [-config dir] [search flags] <text>        (database index)

search flags: -dialect bac|trung|nam -level L0|L1|L2|L25|L3 -match suffix|prefix|any
              -min-syl n -max-syl n -limit n -no-slang -no-proper -json
sources: hnd, wiktionary-vi (vi; its slang senses go to the review queue), oewn (en definitions),
         cmudict (en pronunciations; ingest before oewn),
         slang (-file .csv → submissions pending admin review)
db defaults to $DATABASE_URL.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "playword-cli:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(strings.TrimSpace(usage))
	}
	switch args[0] {
	case "parse":
		return parseCmd(args[1:], out)
	case "rhymes":
		return rhymesCmd(args[1:], out)
	case "migrate":
		return migrateCmd(args[1:], out)
	case "ingest":
		return ingestCmd(args[1:], out)
	case "rebuild-keys":
		return rebuildKeysCmd(args[1:], out)
	case "search":
		return searchCmd(args[1:], out)
	case "freq":
		return freqCmd(args[1:], out)
	case "user-role":
		return userRoleCmd(args[1:], out)
	case "user-delete":
		return userDeleteCmd(args[1:], out)
	case "maintain":
		return maintainCmd(args[1:], out)
	case "dialect-check":
		return dialectCheckCmd(args[1:], out)
	case "healthcheck":
		return healthcheckCmd(args[1:], out)
	}
	return fmt.Errorf("unknown command %q; %s", args[0], strings.TrimSpace(usage))
}

type syllableJSON struct {
	vnphon.Syllable
	Spelled string `json:"spelled"`
}

func parseCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	config := fs.String("config", "configs", "directory holding rhyme_families.yaml and dialects.yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	e, err := rhyme.LoadDir(*config)
	if err != nil {
		return err
	}
	p, err := vnphon.ParsePhrase(text)
	if err != nil {
		return err
	}
	res := struct {
		Text      string                                   `json:"text"`
		Syllables []syllableJSON                           `json:"syllables"`
		Keys      map[rhyme.Dialect]map[rhyme.Level]string `json:"keys"`
	}{Text: text, Keys: map[rhyme.Dialect]map[rhyme.Level]string{}}
	for _, s := range p {
		w, err := vnphon.Render(s)
		if err != nil {
			return err
		}
		res.Syllables = append(res.Syllables, syllableJSON{s, w})
	}
	for _, d := range rhyme.Dialects {
		res.Keys[d] = e.Keys(d, p)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(res)
}

// searchFlags are shared by rhymes (fixture) and search (database).
type searchFlags struct {
	dialect, level, match *string
	minSyl, maxSyl, limit *int
	noSlang, noProper     *bool
	asJSON                *bool
}

func addSearchFlags(fs *flag.FlagSet) searchFlags {
	return searchFlags{
		dialect:  fs.String("dialect", "bac", "bac | trung | nam"),
		level:    fs.String("level", "L25", "loosest level: L0 L1 L2 L25 L3"),
		match:    fs.String("match", "suffix", "suffix | prefix | any"),
		minSyl:   fs.Int("min-syl", 1, "minimum matched syllables"),
		maxSyl:   fs.Int("max-syl", rhyme.MaxSyl, "maximum matched syllables"),
		limit:    fs.Int("limit", 30, "results"),
		noSlang:  fs.Bool("no-slang", false, "exclude slang"),
		noProper: fs.Bool("no-proper", false, "exclude names of people and places"),
		asJSON:   fs.Bool("json", false, "print JSON"),
	}
}

func (f searchFlags) query(text string) (rhyme.Query, error) {
	p, err := vnphon.ParsePhrase(text)
	if err != nil {
		return rhyme.Query{}, err
	}
	q := rhyme.NewQuery(p)
	if q.Dialect, err = rhyme.ParseDialect(*f.dialect); err != nil {
		return q, err
	}
	if q.Level, err = rhyme.ParseLevel(*f.level); err != nil {
		return q, err
	}
	if q.Match, err = rhyme.ParseAlign(*f.match); err != nil {
		return q, err
	}
	q.MinSyl, q.MaxSyl, q.Limit = *f.minSyl, *f.maxSyl, *f.limit
	q.ExcludeSlang, q.ExcludeProper = *f.noSlang, *f.noProper
	return q, nil
}

func rhymesCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rhymes", flag.ContinueOnError)
	config := fs.String("config", "configs", "directory holding rhyme_families.yaml and dialects.yaml")
	lexicon := fs.String("lexicon", "internal/domain/rhyme/testdata/lexicon.tsv", "fixture lexicon")
	sf := addSearchFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := rhyme.LoadDir(*config)
	if err != nil {
		return err
	}
	f, err := os.Open(*lexicon)
	if err != nil {
		return err
	}
	defer f.Close()
	cands, err := rhyme.ReadLexicon(f)
	if err != nil {
		return fmt.Errorf("%s: %w", *lexicon, err)
	}
	q, err := sf.query(strings.Join(fs.Args(), " "))
	if err != nil {
		return err
	}
	resp, err := e.Search(q, cands)
	if err != nil {
		return err
	}
	return printResults(out, resp, *sf.asJSON, nil)
}

// printResults prints ranked results; gloss, when set, adds a definition.
func printResults(out io.Writer, resp rhyme.Response, asJSON bool, gloss func(id int64) string) error {
	if asJSON {
		type row struct {
			Text    string      `json:"text"`
			Matched int         `json:"matched_syllables"`
			Level   rhyme.Level `json:"level"`
			Start   int         `json:"start"`
			End     int         `json:"end"`
			Score   float64     `json:"score"`
			Slang   bool        `json:"is_slang"`
			Gloss   string      `json:"gloss,omitempty"`
		}
		rows := []row{}
		for _, r := range resp.Results {
			g := ""
			if gloss != nil {
				g = gloss(r.Candidate.ID)
			}
			rows = append(rows, row{r.Candidate.Text, r.Matched, r.Level, r.Start, r.End, r.Score, r.Candidate.IsSlang, g})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(map[string]any{"results": rows, "warnings": resp.Warnings})
	}
	for _, w := range resp.Warnings {
		fmt.Fprintln(out, "warning:", w)
	}
	for _, r := range resp.Results {
		words := vnphon.SplitWords(r.Candidate.Text)
		words[r.Start] = "[" + words[r.Start]
		words[r.End-1] += "]"
		extra := ""
		if r.Candidate.IsSlang {
			extra = "  (slang)"
		}
		if gloss != nil {
			extra += "  — " + truncate(gloss(r.Candidate.ID), 70)
		}
		fmt.Fprintf(out, "%6.1f  %d âm  %-3s  %s%s\n", r.Score, r.Matched, r.Level, strings.Join(words, " "), extra)
	}
	if len(resp.Results) == 0 {
		fmt.Fprintln(out, "(không có kết quả)")
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func healthcheckCmd(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: healthcheck <url>")
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(args[0])
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", args[0], resp.Status)
	}
	fmt.Fprintln(out, "ok")
	return nil
}
