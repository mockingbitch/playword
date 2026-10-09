// Package ingest runs Fetch → Parse → Normalize → Validate → Phonemize →
// Upsert for one source (spec §2.1, ADR-003).
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ports"
	"playword/pkg/vnphon"
)

// Skip reasons recorded in ingest_runs.errors.
const (
	SkipBadHeadword  = "bad_headword"
	SkipNoDefinition = "no_definition"
	SkipTooLong      = "too_long"
)

const maxErrorSamples = 1000

// TagProperNoun marks names of people and places (capitalized headwords),
// so search can leave them out.
const TagProperNoun = "proper_noun"

// Pipeline ingests sources into a store.
type Pipeline struct {
	Store     ports.IngestStore
	Engine    *rhyme.Engine
	EN        *en.Pronouncer // required for English sources
	BatchSize int            // default 1000
	Log       *slog.Logger
}

// Options configure one run.
type Options struct {
	DryRun bool
	File   string    // read this local dump instead of fetching
	Since  time.Time // passed to Fetch; an unmodified dump ends the run early
	// Partial means the input is not the source's whole data set (one
	// approved slang, say), so entries it lacks must not be marked stale.
	Partial bool
}

// Report summarizes a run; the same data goes to ingest_runs.
type Report struct {
	RunID       int64          `json:"run_id"`
	Status      string         `json:"status"`
	DryRun      bool           `json:"dry_run"`
	Parsed      int            `json:"parsed"`
	Inserted    int            `json:"inserted"`
	Updated     int            `json:"updated"`
	Unchanged   int            `json:"unchanged"`
	Skipped     int            `json:"skipped"`
	Staled      int            `json:"staled"`
	KeysRebuilt bool           `json:"keys_rebuilt"` // bulk load: rhyme keys rebuilt once at the end
	NoRhyme     int            `json:"no_rhyme"`     // kept for lookup, excluded from rhyme search
	SkipReasons map[string]int `json:"skip_reasons"`
	Errors      []string       `json:"errors,omitempty"`
	Duration    time.Duration  `json:"duration"`
}

func (r *Report) addError(format string, args ...any) {
	if len(r.Errors) < maxErrorSamples {
		r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
	}
}

// Run ingests src once.
func (p *Pipeline) Run(ctx context.Context, src ports.Source, o Options) (*Report, error) {
	start := time.Now()
	log := p.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	batchSize := p.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	rep := &Report{DryRun: o.DryRun, SkipReasons: map[string]int{}}

	r, notModified, err := open(ctx, src, o)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if notModified {
		rep.Status = "ok"
		log.Info("source not modified", "source", src.Name(), "since", o.Since)
		return rep, nil
	}

	// Dedupe by natural key before writing: a word repeated in the dump
	// becomes one entry with all senses.
	byKey := map[string]int{}
	var prepared []ports.PreparedEntry
	err = src.Parse(ctx, r, func(e entry.Entry) error {
		rep.Parsed++
		norm := entry.Normalize(e.Headword)
		if i, dup := byKey[norm]; dup {
			prepared[i].Entry.Senses = append(prepared[i].Entry.Senses, e.Senses...)
			return nil
		}
		if e.Lang == "" {
			e.Lang = src.Lang()
		}
		e.Headword = strings.Join(strings.Fields(e.Headword), " ")
		byKey[norm] = len(prepared)
		prepared = append(prepared, ports.PreparedEntry{Entry: e, Normalized: norm})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", src.Name(), err)
	}

	valid := prepared[:0]
	for _, pe := range prepared {
		if reason := p.prepare(&pe); reason != "" {
			rep.Skipped++
			rep.SkipReasons[reason]++
			if len(rep.Errors) < maxErrorSamples {
				rep.addError("%s: %q", reason, pe.Entry.Headword)
			}
			continue
		}
		if pe.RhymeError != "" {
			rep.NoRhyme++
		}
		valid = append(valid, pe)
	}

	run, err := p.Store.StartRun(ctx, ports.RunOptions{
		Source: src, DryRun: o.DryRun, FullRun: !o.Partial, Keys: ports.KeysConfigOf(p.Engine),
		Engine: p.Engine, Expected: len(valid),
	})
	if err != nil {
		return nil, err
	}
	rep.RunID = run.ID()

	failed := 0
	for i := 0; i < len(valid); i += batchSize {
		batch := valid[i:min(i+batchSize, len(valid))]
		c, err := run.WriteBatch(ctx, batch)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failed++
			rep.addError("batch %d (%q…): %v", i/batchSize, batch[0].Entry.Headword, err)
			log.Error("batch failed", "batch", i/batchSize, "err", err)
			continue
		}
		rep.Inserted += c.Inserted
		rep.Updated += c.Updated
		rep.Unchanged += c.Unchanged
	}

	switch {
	case failed == 0:
		rep.Status = "ok"
	case failed*batchSize < len(valid):
		rep.Status = "partial"
	default:
		rep.Status = "failed"
	}
	errJSON, _ := json.Marshal(map[string]any{"skip_reasons": rep.SkipReasons, "samples": rep.Errors})
	info, err := run.Finish(ctx, ports.RunResult{
		Status:    rep.Status,
		Counts:    ports.BatchCounts{Inserted: rep.Inserted, Updated: rep.Updated, Unchanged: rep.Unchanged},
		Skipped:   rep.Skipped,
		Errors:    errJSON,
		MarkStale: !o.Partial,
	})
	if err != nil {
		return nil, err
	}
	rep.Staled, rep.KeysRebuilt = info.Staled, info.KeysRebuilt
	rep.Duration = time.Since(start)
	if o.DryRun {
		rep.Status = "dry_run"
	}
	log.Info("ingest finished", "source", src.Name(), "run", rep.RunID, "status", rep.Status,
		"inserted", rep.Inserted, "updated", rep.Updated, "unchanged", rep.Unchanged,
		"skipped", rep.Skipped, "staled", rep.Staled, "took", rep.Duration)
	if rep.Status == "failed" {
		return rep, errors.New("every batch failed")
	}
	return rep, nil
}

// prepare validates pe and fills in syllables and rhyme keys; it returns a
// skip reason, or "" when the entry is kept.
func (p *Pipeline) prepare(pe *ports.PreparedEntry) string {
	e := &pe.Entry
	if !validHeadword(e.Headword) {
		return SkipBadHeadword
	}
	if !e.HasDefinition() {
		return SkipNoDefinition
	}
	words := vnphon.SplitWords(e.Headword)
	pe.NSyllables = len(words)
	if pe.NSyllables > rhyme.MaxSyl {
		return SkipTooLong
	}
	if r := []rune(e.Headword); unicode.IsUpper(r[0]) && !slices.Contains(e.Tags, TagProperNoun) {
		e.Tags = append(e.Tags, TagProperNoun)
	}
	pe.Hash = e.ContentHash()
	pe.Confidence = 1
	switch e.Lang {
	case entry.EN:
		p.prepareEN(pe)
		return ""
	case entry.VI:
	default:
		return ""
	}
	syl, err := vnphon.ParsePhrase(pronounced(e))
	if err != nil {
		var se *vnphon.SyllableError
		if errors.As(err, &se) {
			pe.RhymeError = fmt.Sprintf("%s: %q", se.Reason, se.Input)
		} else {
			pe.RhymeError = err.Error()
		}
		return ""
	}
	pe.Syllables = syl
	pe.Keys = p.Engine.WindowKeys(syl)
	return ""
}

// validHeadword rejects lines where the dump put a definition or a list of
// names in the headword column.
func validHeadword(h string) bool {
	if h == "" || strings.HasPrefix(h, "-") {
		return false
	}
	return !strings.ContainsAny(h, ".,:;@()[]0123456789\"")
}

func open(ctx context.Context, src ports.Source, o Options) (io.ReadCloser, bool, error) {
	if o.File != "" {
		f, err := os.Open(o.File)
		return f, false, err
	}
	rc, meta, err := src.Fetch(ctx, o.Since)
	if err != nil {
		return nil, false, err
	}
	return rc, meta.NotModified, nil
}

// prepareEN pronounces an English headword (CMUdict, or a derived form with
// lower confidence) and computes its segments and keys.
func (p *Pipeline) prepareEN(pe *ports.PreparedEntry) {
	if p.EN == nil {
		pe.RhymeError = "no pronunciation dictionary loaded"
		return
	}
	phones, conf, err := p.EN.Pronounce(pronounced(&pe.Entry))
	if err != nil {
		pe.RhymeError = err.Error()
		return
	}
	segs, err := en.Segments(phones)
	if err != nil {
		pe.RhymeError = err.Error()
		return
	}
	pe.Segs, pe.Confidence, pe.Keys = segs, conf, rhyme.WindowKeysEN(segs)
}

// pronounced is the text rhyme keys come from: the respelling of a loanword
// or slang when there is one, else the headword.
func pronounced(e *entry.Entry) string {
	if e.Respelling != "" {
		return e.Respelling
	}
	return e.Headword
}
