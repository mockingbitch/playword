// Package ports declares the interfaces the application depends on;
// adapters implement them.
package ports

import (
	"context"
	"io"
	"time"

	"playword/internal/domain/entry"
)

// SourceKind says what a source contributes (ADR-003 D2).
type SourceKind string

// Source kinds.
const (
	KindDictionary    SourceKind = "dictionary"
	KindFrequency     SourceKind = "frequency"
	KindPronunciation SourceKind = "pronunciation"
	KindSlang         SourceKind = "slang"
)

// Meta describes a fetched dump.
type Meta struct {
	URL          string
	ETag         string
	LastModified time.Time
	NotModified  bool // nothing new since the requested time
}

// Source is one upstream data set (spec §2.1, ADR-003 D2). Dictionary
// sources emit entries; frequency/pronunciation sources will get their own
// emit types when they are added.
type Source interface {
	Name() string
	Kind() SourceKind
	Lang() entry.Lang
	License() string
	URL() string
	Fetch(ctx context.Context, since time.Time) (io.ReadCloser, Meta, error)
	Parse(ctx context.Context, r io.Reader, emit func(entry.Entry) error) error
}
