package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/adapters/postgres"
	"playword/internal/adapters/sources/cmudict"
	"playword/internal/adapters/sources/corpusfreq"
	"playword/internal/adapters/sources/registry"
	"playword/internal/app"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ingest"
	"playword/internal/ports"
)

func dbFlag(fs *flag.FlagSet) *string {
	return fs.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL DSN")
}

func openStore(ctx context.Context, dsn string) (*postgres.Store, error) {
	if dsn == "" {
		return nil, errors.New("no database: pass -db or set DATABASE_URL")
	}
	return postgres.Open(ctx, dsn)
}

func sources() map[string]ports.Source { return registry.Dictionaries() }

func migrateCmd(args []string, _ io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dsn := dbFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	cmd := "up"
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}
	return st.Migrate(ctx, cmd, fs.Args()[min(1, fs.NArg()):]...)
}

func ingestCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	dsn := dbFlag(fs)
	config := fs.String("config", "configs", "rhyme config directory")
	file := fs.String("file", "", "read a local dump instead of downloading")
	dry := fs.Bool("dry-run", false, "run everything, then roll back")
	since := fs.String("since", "", "only fetch if the dump changed after this time (RFC 3339)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var sinceT time.Time
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			return fmt.Errorf("-since: %w", err)
		}
		sinceT = t
	}
	ctx := context.Background()
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	switch fs.Arg(0) {
	case "cmudict":
		return ingestCMUdict(ctx, st, *file, *dry, out)
	case "slang":
		return ingestSlangCSV(ctx, st, *file, out)
	}
	src, ok := sources()[fs.Arg(0)]
	if !ok {
		return fmt.Errorf("unknown source %q (have: %s, cmudict, slang)", fs.Arg(0), strings.Join(registry.Names(), ", "))
	}
	e, err := rhyme.LoadDir(*config)
	if err != nil {
		return err
	}
	p := ingest.Pipeline{Store: st, Engine: e, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	if src.Lang() == entry.EN {
		prons, err := st.Pronunciations(ctx, entry.EN)
		if err != nil {
			return err
		}
		if len(prons) == 0 {
			return errors.New("no English pronunciations yet: run `playword-cli ingest cmudict` first")
		}
		p.EN = en.NewPronouncer(prons)
	}
	// Slang found in a dictionary goes to the review queue, never straight in.
	slangSvc := app.NewSlang(st, p, nil)
	run := src
	if !*dry {
		run = slangSvc.DivertSlang(ctx, src)
	}
	rep, err := p.Run(ctx, run, ingest.Options{DryRun: *dry, File: *file, Since: sinceT})
	if rep != nil {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		rep.Errors = rep.Errors[:min(len(rep.Errors), 10)] // full list is in ingest_runs.errors
		_ = enc.Encode(rep)
	}
	return err
}

func rebuildKeysCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("rebuild-keys", flag.ContinueOnError)
	dsn := dbFlag(fs)
	config := fs.String("config", "configs", "rhyme config directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	e, err := rhyme.LoadDir(*config)
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	start := time.Now()
	n, err := st.RebuildKeys(ctx, e)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "rebuilt %d rhyme keys with config %.12s in %s\n", n, e.ConfigID(), time.Since(start).Round(time.Millisecond))
	return nil
}

func searchCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	dsn := dbFlag(fs)
	config := fs.String("config", "configs", "rhyme config directory")
	sf := addSearchFlags(fs)
	queries := fs.String("queries", "", "file of queries, one per line: run them all against one index build and print TSV (query, rank, text, matched, level, score, identical, distance, proper) — for comparing rankings")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	e, err := rhyme.LoadDir(*config)
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()

	var h rhymeindex.Holder
	start := time.Now()
	snap, err := h.Rebuild(ctx, st, e)
	if err != nil {
		return err
	}
	built := time.Since(start)
	if *queries != "" {
		return searchBatch(out, snap, e, sf, *queries)
	}
	q, err := sf.query(strings.Join(fs.Args(), " "))
	if err != nil {
		return err
	}
	start = time.Now()
	resp, err := snap.Search(e, q)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "index: %d entries built in %s; query %s\n", snap.Len(), built.Round(time.Millisecond), time.Since(start).Round(time.Microsecond))
	return printResults(out, resp, *sf.asJSON, func(id int64) string {
		if en := snap.Entry(id); en != nil && len(en.Senses) > 0 {
			return en.Senses[0].Gloss
		}
		return ""
	})
}

func freqCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("freq", flag.ContinueOnError)
	dsn := dbFlag(fs)
	corpus := fs.String("corpus", "", "plain-text corpus, e.g. from .scripts/viwiki-text.sh")
	list := fs.String("list", "", `"word count" frequency list (FrequencyWords); multiword phrases are estimated`)
	langFlag := fs.String("lang", "vi", "language of the vocabulary: vi | en")
	name := fs.String("name", "viwiki-freq", "source name")
	license := fs.String("license", "CC BY-SA 4.0 — counts derived from Vietnamese Wikipedia (wikimedia/wikipedia 20231101.vi)", "license of the corpus")
	srcURL := fs.String("url", "https://huggingface.co/datasets/wikimedia/wikipedia", "corpus URL")
	workers := fs.Int("workers", runtime.NumCPU(), "counting goroutines")
	dry := fs.Bool("dry-run", false, "count, then roll back")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*corpus == "") == (*list == "") {
		return errors.New("pass exactly one of -corpus or -list")
	}
	lang := entry.Lang(*langFlag)
	ctx := context.Background()
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	vocab, err := st.Vocab(ctx, lang)
	if err != nil {
		return err
	}
	if *list != "" {
		return freqFromList(ctx, st, lang, vocab, *list, *name, *license, *srcURL, *dry, out)
	}
	c, err := corpusfreq.NewCounter(vocab)
	if err != nil {
		return err
	}
	f, err := os.Open(*corpus)
	if err != nil {
		return err
	}
	defer f.Close()
	start := time.Now()
	res, err := c.Count(ctx, f, *workers)
	if err != nil {
		return err
	}
	rows := make([]postgres.FreqRow, 0, len(res.Counts))
	for p, n := range res.Counts {
		rows = append(rows, postgres.FreqRow{Normalized: p, Zipf: res.Zipf(n)})
	}
	stats := map[string]any{"tokens": res.Tokens, "vocab": len(vocab), "seen": len(rows), "took": time.Since(start).String()}
	runID, err := st.ReplaceWordFreq(ctx, postgres.FreqSource{Name: *name, License: *license, URL: *srcURL, Lang: lang}, rows, stats, *dry)
	if err != nil {
		return err
	}
	stats["run_id"], stats["dry_run"] = runID, *dry
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(stats)
}

// ingestCMUdict replaces the English pronunciations (a pronunciation source,
// ADR-003 D2: it creates no entries).
func ingestCMUdict(ctx context.Context, st *postgres.Store, file string, dry bool, out io.Writer) error {
	src := cmudict.New("", nil)
	var r io.ReadCloser
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		r = f
	} else {
		body, _, err := src.Fetch(ctx, time.Time{})
		if err != nil {
			return err
		}
		r = body
	}
	defer r.Close()
	var rows []postgres.PronRow
	err := cmudict.Parse(ctx, r, func(word string, phones []string) error {
		rows = append(rows, postgres.PronRow{Normalized: word, Phonemes: phones, Confidence: 1})
		return nil
	})
	if err != nil {
		return err
	}
	runID, err := st.ReplacePronunciations(ctx, postgres.FreqSource{Name: src.Name(), License: cmudict.License,
		URL: src.URL(), Lang: entry.EN}, rows, dry)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "{\"run_id\": %d, \"pronunciations\": %d, \"dry_run\": %t}\n", runID, len(rows), dry)
	return nil
}

func freqFromList(ctx context.Context, st *postgres.Store, lang entry.Lang, vocab []string, path, name, license, url string, dry bool, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	words, err := corpusfreq.WordListZipf(f)
	if err != nil {
		return err
	}
	var rows []postgres.FreqRow
	for _, p := range vocab {
		if z, ok := corpusfreq.PhraseZipf(p, words); ok {
			rows = append(rows, postgres.FreqRow{Normalized: p, Zipf: z})
		}
	}
	stats := map[string]any{"list_words": len(words), "vocab": len(vocab), "seen": len(rows)}
	runID, err := st.ReplaceWordFreq(ctx, postgres.FreqSource{Name: name, License: license, URL: url, Lang: lang}, rows, stats, dry)
	if err != nil {
		return err
	}
	stats["run_id"], stats["dry_run"] = runID, dry
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(stats)
}

func userRoleCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("user-role", flag.ContinueOnError)
	dsn := dbFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 || (fs.Arg(1) != "user" && fs.Arg(1) != "admin") {
		return errors.New("usage: user-role <email> <user|admin>")
	}
	ctx := context.Background()
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, _, err := st.UserByEmail(ctx, strings.ToLower(fs.Arg(0))); err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	if err := st.SetRole(ctx, strings.ToLower(fs.Arg(0)), fs.Arg(1)); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s is now %s\n", fs.Arg(0), fs.Arg(1))
	return nil
}

// ingestSlangCSV queues a slang file for admin review (spec §2.2: every slang
// starts pending).
func ingestSlangCSV(ctx context.Context, st *postgres.Store, file string, out io.Writer) error {
	if file == "" {
		return errors.New("slang needs -file path.csv (columns: headword, lang, gloss, example, respelling, tags)")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	n, errs := app.NewSlang(st, ingest.Pipeline{}, nil).ImportCSV(ctx, f, "csv:"+filepath.Base(file))
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e)
	}
	fmt.Fprintf(out, "{\"queued\": %d, \"errors\": %d}\n", n, len(errs))
	return nil
}

// userDeleteCmd erases an account on request (runbook: account deletion):
// books and sessions cascade, events are anonymized.
func userDeleteCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("user-delete", flag.ContinueOnError)
	dsn := dbFlag(fs)
	confirm := fs.Bool("confirm", false, "required: the deletion cannot be undone")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || !*confirm {
		return errors.New("usage: user-delete -confirm <email>")
	}
	ctx := context.Background()
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	u, _, err := st.UserByEmail(ctx, strings.ToLower(fs.Arg(0)))
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted %s (%s); events anonymized\n", u.Email, u.ID)
	return nil
}

// maintainCmd runs the periodic jobs once, for cron-based deployments.
func maintainCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("maintain", flag.ContinueOnError)
	dsn := dbFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	st, err := openStore(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	made, dropped, err := st.MaintainPartitions(ctx, 2, 180)
	if err != nil {
		return err
	}
	if err := st.RefreshViews(ctx); err != nil {
		return err
	}
	sessions, err := st.DeleteExpiredSessions(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "partitions created %d, dropped %d; views refreshed; expired sessions removed %d\n", made, dropped, sessions)
	return nil
}

// searchBatch runs every query of a file against one snapshot and prints
// one TSV row per result, so two rankings can be diffed line by line.
func searchBatch(out io.Writer, snap *rhymeindex.Snapshot, e *rhyme.Engine, sf searchFlags, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "query\trank\ttext\tmatched\tlevel\tscore\tidentical\tdistance\tproper")
	for _, line := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		q, err := sf.query(text)
		if err != nil {
			fmt.Fprintf(out, "%s\t0\t(%v)\t0\t-\t0\t0\t0\t0\n", text, err)
			continue
		}
		resp, err := snap.Search(e, q)
		if err != nil {
			fmt.Fprintf(out, "%s\t0\t(%v)\t0\t-\t0\t0\t0\t0\n", text, err)
			continue
		}
		for i, r := range resp.Results {
			proper := 0
			if r.Candidate.IsProper {
				proper = 1
			}
			fmt.Fprintf(out, "%s\t%d\t%s\t%d\t%s\t%.1f\t%d\t%d\t%d\n", text, i+1, r.Candidate.Text, r.Matched, r.Level, r.Score, r.Identical, r.Distance, proper)
		}
	}
	return nil
}
