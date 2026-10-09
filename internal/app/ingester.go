package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ingest"
	"playword/internal/ports"
)

// ErrBusy means another server is already ingesting that source.
var ErrBusy = errors.New("ingest already running")

// IngestBackend is what Ingester needs from storage beyond the pipeline.
type IngestBackend interface {
	TryLock(ctx context.Context, key string, fn func() error) (bool, error)
	LastSuccessfulRun(ctx context.Context, source string) (time.Time, error)
	Pronunciations(ctx context.Context, lang entry.Lang) (map[string][]string, error)
}

// Ingester runs a dictionary source on demand (admin) or on a schedule,
// under a cluster-wide lock and with a conditional download.
type Ingester struct {
	backend  IngestBackend
	pipeline ingest.Pipeline
	sources  map[string]ports.Source
	slang    *Slang // queues slang found in dictionary sources; nil keeps it out
}

// WithSlang routes slang entries of dictionary sources to review.
func (i *Ingester) WithSlang(s *Slang) *Ingester { i.slang = s; return i }

// NewIngester wires the sources that may be run.
func NewIngester(b IngestBackend, p ingest.Pipeline, sources map[string]ports.Source) *Ingester {
	return &Ingester{backend: b, pipeline: p, sources: sources}
}

// Run ingests one source. It returns ErrBusy if another node holds the lock,
// and a report with status "ok" and no counts if the dump is unchanged.
func (i *Ingester) Run(ctx context.Context, name string, dryRun bool) (*ingest.Report, error) {
	src, ok := i.sources[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown source %q", ErrNotFound, name)
	}
	var rep *ingest.Report
	ran, err := i.backend.TryLock(ctx, "ingest:"+name, func() error {
		p := i.pipeline
		if src.Lang() == entry.EN {
			prons, err := i.backend.Pronunciations(ctx, entry.EN)
			if err != nil {
				return err
			}
			p.EN = en.NewPronouncer(prons)
		}
		since, err := i.backend.LastSuccessfulRun(ctx, name)
		if err != nil {
			return err
		}
		var run ports.Source = dropSlang{src}
		if i.slang != nil && !dryRun {
			run = i.slang.DivertSlang(ctx, src)
		}
		rep, err = p.Run(ctx, run, ingest.Options{DryRun: dryRun, Since: since})
		return err
	})
	if err != nil {
		return rep, err
	}
	if !ran {
		return nil, ErrBusy
	}
	return rep, nil
}

// dropSlang keeps slang entries out when there is nowhere to queue them.
type dropSlang struct{ ports.Source }

func (d dropSlang) Parse(ctx context.Context, r io.Reader, emit func(entry.Entry) error) error {
	return d.Source.Parse(ctx, r, func(e entry.Entry) error {
		if e.IsSlang {
			return nil
		}
		return emit(e)
	})
}
