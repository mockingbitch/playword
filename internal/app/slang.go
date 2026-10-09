package app

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"playword/internal/domain/account"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme/en"
	"playword/internal/domain/slang"
	"playword/internal/ingest"
	"playword/internal/ports"
)

// CommunitySource is the source name of approved slang.
const CommunitySource = "community"

// Slang implements slang submission, review and trends (M6).
type Slang struct {
	store    ports.SlangStore
	pipeline ingest.Pipeline
	// pron loads the English pronouncer when an English slang is approved.
	pron func(context.Context) (*en.Pronouncer, error)
}

// NewSlang wires slang review; approvals go through the ingest pipeline so
// they are normalized, phonemized and keyed like any other entry.
func NewSlang(store ports.SlangStore, p ingest.Pipeline, pron func(context.Context) (*en.Pronouncer, error)) *Slang {
	return &Slang{store: store, pipeline: p, pron: pron}
}

// Submit stores a pending submission.
func (s *Slang) Submit(ctx context.Context, sub *slang.Submission) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	return s.store.CreateSubmission(ctx, sub)
}

// List returns submissions in a status, oldest first.
func (s *Slang) List(ctx context.Context, status string, limit int) ([]slang.Submission, error) {
	switch status {
	case "", slang.Pending:
		status = slang.Pending
	case slang.Approved, slang.Rejected:
	default:
		return nil, fmt.Errorf("%w: status", slang.ErrInvalid)
	}
	return s.store.Submissions(ctx, status, min(max(limit, 1), 500))
}

// communitySource feeds one approved submission to the pipeline.
type communitySource struct {
	lang entry.Lang
	e    entry.Entry
}

func (c communitySource) Name() string           { return CommunitySource }
func (c communitySource) Kind() ports.SourceKind { return ports.KindSlang }
func (c communitySource) Lang() entry.Lang       { return c.lang }
func (c communitySource) URL() string            { return "" }
func (c communitySource) License() string {
	return "Playword community contributions, reviewed by admins"
}
func (c communitySource) Fetch(context.Context, time.Time) (io.ReadCloser, ports.Meta, error) {
	return io.NopCloser(strings.NewReader("")), ports.Meta{}, nil
}
func (c communitySource) Parse(_ context.Context, _ io.Reader, emit func(entry.Entry) error) error {
	return emit(c.e)
}

// Approve turns a pending submission into a dictionary entry with trend 1.0.
func (s *Slang) Approve(ctx context.Context, reviewer string, id int64) (*slang.Submission, error) {
	sub, err := s.store.Submission(ctx, id)
	if err != nil {
		return nil, err
	}
	if sub.Status != slang.Pending {
		return nil, fmt.Errorf("%w: submission is already %s", slang.ErrInvalid, sub.Status)
	}
	p := s.pipeline
	if sub.Lang == entry.EN && s.pron != nil {
		if p.EN, err = s.pron(ctx); err != nil {
			return nil, err
		}
	}
	rep, err := p.Run(ctx, communitySource{lang: sub.Lang, e: sub.Entry()}, ingest.Options{Partial: true})
	if err != nil {
		return nil, err
	}
	if rep.Skipped > 0 {
		return nil, fmt.Errorf("%w: %v", slang.ErrInvalid, rep.Errors)
	}
	entryID, err := s.store.EntryIDBySource(ctx, sub.Lang, entry.Normalize(sub.Headword), CommunitySource)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetTrend(ctx, entryID, 1.0); err != nil {
		return nil, err
	}
	if err := s.store.ReviewSubmission(ctx, id, slang.Approved, entryID, reviewer); err != nil {
		return nil, err
	}
	sub.Status, sub.EntryID = slang.Approved, entryID
	return sub, s.store.NotifyIndex(ctx)
}

// Reject closes a pending submission.
func (s *Slang) Reject(ctx context.Context, reviewer string, id int64) error {
	err := s.store.ReviewSubmission(ctx, id, slang.Rejected, 0, reviewer)
	if errors.Is(err, account.ErrNotFound) {
		if _, gerr := s.store.Submission(ctx, id); gerr == nil {
			return fmt.Errorf("%w: submission is not pending", slang.ErrInvalid)
		}
	}
	return err
}

// Bump raises (or lowers) the trend of an approved slang entry.
func (s *Slang) Bump(ctx context.Context, entryID int64, delta float64) (float64, error) {
	if delta < -10 || delta > 10 {
		return 0, fmt.Errorf("%w: delta must be within ±10", slang.ErrInvalid)
	}
	v, err := s.store.BumpTrend(ctx, entryID, delta)
	if err != nil {
		return 0, err
	}
	return v, s.store.NotifyIndex(ctx)
}

// ImportCSV queues the rows of a slang file for review. Columns, with a
// header row: headword, lang, gloss, example, respelling, tags (";"-separated).
// Invalid rows are reported and skipped.
func (s *Slang) ImportCSV(ctx context.Context, r io.Reader, submittedBy string) (int, []error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return 0, []error{fmt.Errorf("header: %w", err)}
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	if _, ok := col["headword"]; !ok {
		return 0, []error{errors.New(`header must name at least "headword" and "gloss"`)}
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	n := 0
	var errs []error
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", line, err))
			continue
		}
		sub := &slang.Submission{Headword: get(rec, "headword"), Lang: entry.Lang(get(rec, "lang")), Gloss: get(rec, "gloss"),
			Example: get(rec, "example"), Respelling: get(rec, "respelling"), SubmittedBy: submittedBy}
		for _, t := range strings.Split(get(rec, "tags"), ";") {
			if t = strings.TrimSpace(t); t != "" {
				sub.Tags = append(sub.Tags, t)
			}
		}
		if err := s.Submit(ctx, sub); err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", line, err))
			continue
		}
		n++
	}
	return n, errs
}

// QueueEntry queues a slang entry found in a dictionary source for review,
// once: a later run of the same source does not queue it again.
func (s *Slang) QueueEntry(ctx context.Context, source string, e entry.Entry) error {
	by := "source:" + source
	headword := strings.Join(strings.Fields(e.Headword), " ")
	if dup, err := s.store.SubmissionExists(ctx, e.Lang, headword, by); err != nil || dup {
		return err
	}
	sub := &slang.Submission{Headword: headword, Lang: e.Lang, SubmittedBy: by, Tags: e.Tags}
	var glosses []string
	for _, sn := range e.Senses {
		glosses = append(glosses, sn.Gloss)
		if sub.Example == "" && len(sn.Examples) > 0 {
			sub.Example = sn.Examples[0]
		}
	}
	sub.Gloss = strings.Join(glosses, "; ")
	if len([]rune(sub.Gloss)) > 500 {
		sub.Gloss = string([]rune(sub.Gloss)[:499]) + "…"
	}
	if err := s.Submit(ctx, sub); errors.Is(err, slang.ErrInvalid) {
		return nil // not a usable submission (too long a headword, say); skip it
	} else if err != nil {
		return err
	}
	return nil
}

// divertSlang passes a source's ordinary entries on and queues its slang
// entries for review (spec §2.2: every slang starts pending).
type divertSlang struct {
	ports.Source
	ctx   context.Context
	slang *Slang
	n     int
}

// DivertSlang wraps src so its IsSlang entries go to the review queue.
func (s *Slang) DivertSlang(ctx context.Context, src ports.Source) ports.Source {
	return &divertSlang{Source: src, ctx: ctx, slang: s}
}

func (d *divertSlang) Parse(ctx context.Context, r io.Reader, emit func(entry.Entry) error) error {
	return d.Source.Parse(ctx, r, func(e entry.Entry) error {
		if !e.IsSlang {
			return emit(e)
		}
		d.n++
		return d.slang.QueueEntry(d.ctx, d.Name(), e)
	})
}
