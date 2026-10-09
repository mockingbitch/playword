package apptest

import (
	"context"
	"slices"
	"sync"
	"time"

	"playword/internal/domain/account"
	"playword/internal/domain/entry"
	"playword/internal/domain/slang"
	"playword/internal/ports"
)

// Ingest is an in-memory ports.IngestStore that keeps written entries.
type Ingest struct {
	mu      sync.Mutex
	Entries map[string]int64 // "lang|normalized" -> id
	Written []ports.PreparedEntry
	nextID  int64
}

// NewIngest returns an empty store.
func NewIngest() *Ingest { return &Ingest{Entries: map[string]int64{}, nextID: 1000} }

var _ ports.IngestStore = (*Ingest)(nil)

func (i *Ingest) StartRun(context.Context, ports.RunOptions) (ports.IngestRun, error) {
	return &ingestRun{i}, nil
}

type ingestRun struct{ i *Ingest }

func (r *ingestRun) ID() int64 { return 1 }

func (r *ingestRun) WriteBatch(_ context.Context, batch []ports.PreparedEntry) (ports.BatchCounts, error) {
	r.i.mu.Lock()
	defer r.i.mu.Unlock()
	var c ports.BatchCounts
	for _, p := range batch {
		k := string(p.Entry.Lang) + "|" + p.Normalized
		if _, ok := r.i.Entries[k]; ok {
			c.Updated++
		} else {
			r.i.nextID++
			r.i.Entries[k] = r.i.nextID
			c.Inserted++
		}
		r.i.Written = append(r.i.Written, p)
	}
	return c, nil
}

func (r *ingestRun) Finish(context.Context, ports.RunResult) (ports.FinishInfo, error) {
	return ports.FinishInfo{}, nil
}

// Slang is an in-memory ports.SlangStore; approved entries are looked up in
// Ingest.
type Slang struct {
	mu      sync.Mutex
	subs    []slang.Submission
	Trends  map[int64]float64
	Ingest  *Ingest
	Notices int
}

// NewSlang returns an empty store backed by ing for entry lookups.
func NewSlang(ing *Ingest) *Slang { return &Slang{Trends: map[int64]float64{}, Ingest: ing} }

var _ ports.SlangStore = (*Slang)(nil)

func (s *Slang) CreateSubmission(_ context.Context, sub *slang.Submission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub.ID, sub.Status, sub.CreatedAt = int64(len(s.subs)+1), slang.Pending, time.Now()
	s.subs = append(s.subs, *sub)
	return nil
}

func (s *Slang) Submissions(_ context.Context, status string, limit int) ([]slang.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []slang.Submission
	for _, x := range s.subs {
		if x.Status == status && len(out) < limit {
			out = append(out, x)
		}
	}
	return out, nil
}

func (s *Slang) Submission(_ context.Context, id int64) (*slang.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 1 || int(id) > len(s.subs) {
		return nil, account.ErrNotFound
	}
	c := s.subs[id-1]
	return &c, nil
}

func (s *Slang) ReviewSubmission(_ context.Context, id int64, status string, entryID int64, reviewer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 1 || int(id) > len(s.subs) || s.subs[id-1].Status != slang.Pending {
		return account.ErrNotFound
	}
	s.subs[id-1].Status, s.subs[id-1].EntryID, s.subs[id-1].ReviewedBy = status, entryID, reviewer
	return nil
}

func (s *Slang) EntryIDBySource(_ context.Context, lang entry.Lang, normalized, _ string) (int64, error) {
	s.Ingest.mu.Lock()
	defer s.Ingest.mu.Unlock()
	id, ok := s.Ingest.Entries[string(lang)+"|"+normalized]
	if !ok {
		return 0, account.ErrNotFound
	}
	return id, nil
}

func (s *Slang) SetTrend(_ context.Context, id int64, v float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Trends[id] = v
	return nil
}

func (s *Slang) BumpTrend(_ context.Context, id int64, d float64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Trends[id]; !ok {
		return 0, account.ErrNotFound
	}
	s.Trends[id] += d
	return s.Trends[id], nil
}

func (s *Slang) NotifyIndex(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Notices++
	return nil
}

// Statuses lists submission statuses in id order (assertions).
func (s *Slang) Statuses() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, x := range s.subs {
		out = append(out, x.Status)
	}
	return slices.Clip(out)
}

func (s *Slang) SubmissionExists(_ context.Context, lang entry.Lang, headword, by string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.subs {
		if x.Lang == lang && x.Headword == headword && x.SubmittedBy == by {
			return true, nil
		}
	}
	return false, nil
}
