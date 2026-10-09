package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"playword/internal/domain/analytics"
)

type sink struct {
	mu      sync.Mutex
	got     []analytics.Event
	block   chan struct{} // when set, InsertEvents waits on it
	fail    bool
	batches int
}

func (s *sink) InsertEvents(_ context.Context, ev []analytics.Event) error {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches++
	if s.fail {
		return errors.New("db down")
	}
	s.got = append(s.got, ev...)
	return nil
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func TestRecorderBatchesAndFlushes(t *testing.T) {
	sk := &sink{}
	r := NewRecorder(sk, 100, nil)
	r.batch, r.every = 10, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	for range 25 {
		r.Record(analytics.Event{Type: analytics.Search})
	}
	deadline := time.Now().Add(2 * time.Second)
	for sk.count() < 25 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	r.Record(analytics.Event{Type: analytics.Login})
	cancel()
	<-done
	if sk.count() != 26 || r.Recorded() != 26 || r.Dropped() != 0 {
		t.Errorf("written %d, recorded %d, dropped %d", sk.count(), r.Recorded(), r.Dropped())
	}
	if sk.got[0].TS.IsZero() {
		t.Error("timestamps are filled in")
	}
}

// A full buffer drops events and counts them; Record never blocks.
func TestRecorderDropsWhenFull(t *testing.T) {
	sk := &sink{block: make(chan struct{})}
	r := NewRecorder(sk, 5, nil)
	r.batch = 1
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	start := time.Now()
	for range 1000 {
		r.Record(analytics.Event{Type: analytics.Search})
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("Record blocked for %v", time.Since(start))
	}
	if r.Dropped() < 990 {
		t.Errorf("dropped = %d", r.Dropped())
	}
	close(sk.block)
	cancel()
	var nilRec *Recorder
	nilRec.Record(analytics.Event{}) // analytics disabled: no panic
	if nilRec.Dropped() != 0 || nilRec.Recorded() != 0 || nilRec.Failed() != 0 {
		t.Error("nil recorder counters")
	}
}

func TestRecorderCountsFailures(t *testing.T) {
	sk := &sink{fail: true}
	r := NewRecorder(sk, 10, nil)
	r.every = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	r.Record(analytics.Event{Type: analytics.Search})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if r.Failed() != 1 || r.Recorded() != 0 {
		t.Errorf("failed %d recorded %d", r.Failed(), r.Recorded())
	}
}

func TestParseWindow(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "36h": 36 * time.Hour} {
		if got, err := ParseWindow(in); err != nil || got != want {
			t.Errorf("%q = %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"x", "0d", "-3d", "400d", "d"} {
		if _, err := ParseWindow(bad); !errors.Is(err, ErrInvalidParam) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
