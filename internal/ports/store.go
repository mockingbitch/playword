package ports

import (
	"context"
	"errors"

	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/pkg/vnphon"
)

// ErrKeysConfigMismatch means the rhyme keys in storage were produced by a
// different YAML than the running engine; run `playword-cli rebuild-keys`.
var ErrKeysConfigMismatch = errors.New("stored rhyme keys were built from a different rhyme config")

// KeysConfig identifies the rhyme YAML that keys are computed with.
type KeysConfig struct {
	ID, FamiliesYAML, DialectsYAML string
}

// KeysConfigOf describes the config e was loaded from.
func KeysConfigOf(e *rhyme.Engine) KeysConfig {
	f, d := e.ConfigSource()
	return KeysConfig{ID: e.ConfigID(), FamiliesYAML: f, DialectsYAML: d}
}

// PreparedEntry is an entry after normalization, validation and phonemizing,
// ready to be written.
type PreparedEntry struct {
	Entry      entry.Entry
	Normalized string
	NSyllables int
	Syllables  []vnphon.Syllable // vi; nil when RhymeError is set
	Segs       []en.Seg          // en; nil when RhymeError is set
	Confidence float64           // phonetic confidence (en: 0.7 when derived)
	Keys       []rhyme.WindowKey
	RhymeError string
	Hash       []byte
}

// RunOptions configure one ingest run.
type RunOptions struct {
	Source  Source
	DryRun  bool // everything happens in one transaction that is rolled back
	FullRun bool // the dump is complete, so entries not seen may be marked stale
	Keys    KeysConfig
	// Engine computes rhyme keys; Expected is how many entries the run will
	// write. When most of them are new, keys are rebuilt once at the end
	// instead of per batch (bulk load, ~6x faster).
	Engine   *rhyme.Engine
	Expected int
}

// BatchCounts tallies what a batch (or a run) did.
type BatchCounts struct {
	Inserted, Updated, Unchanged int
}

// RunResult is what the pipeline reports when a run ends.
type RunResult struct {
	Status    string // ok | partial | failed
	Counts    BatchCounts
	Skipped   int
	Errors    []byte // JSON
	MarkStale bool
}

// IngestStore starts ingest runs.
type IngestStore interface {
	StartRun(ctx context.Context, o RunOptions) (IngestRun, error)
}

// IngestRun writes one run: batches, then Finish.
type IngestRun interface {
	ID() int64
	WriteBatch(ctx context.Context, batch []PreparedEntry) (BatchCounts, error)
	// Finish records the run and, unless it is a dry run, commits and
	// notifies index builders. It returns how many entries became stale.
	Finish(ctx context.Context, r RunResult) (FinishInfo, error)
}

// FinishInfo is what Finish did beyond recording the run.
type FinishInfo struct {
	Staled      int
	KeysRebuilt bool
}

// IndexData is everything the in-memory index is built from, all languages.
type IndexData struct {
	ConfigID       string
	Entries        []IndexEntry
	Syllables      map[int64][]vnphon.Syllable // vi entries
	Segments       map[int64][]en.Seg          // en entries
	Senses         map[int64][]entry.Sense
	Pronunciations map[string][]string // en: lowercase word -> phones, for queries
}

// IndexEntry is one approved entry as the index needs it.
type IndexEntry struct {
	ID         int64
	Lang       entry.Lang
	Confidence float64
	Headword   string
	Normalized string
	POS        string
	Source     string
	IsSlang    bool
	Tags       []string
	Trend      float64
	Zipf       float64
}

// IndexLoader reads IndexData from storage.
type IndexLoader interface {
	LoadIndex(ctx context.Context) (*IndexData, error)
}
