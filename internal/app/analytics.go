package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"playword/internal/domain/analytics"
	"playword/internal/ports"
)

// Recorder collects events without slowing requests (spec addendum §3):
// Record never blocks; when the buffer is full the event is dropped and
// counted. A worker writes batches.
type Recorder struct {
	ch       chan analytics.Event
	sink     ports.EventSink
	log      *slog.Logger
	batch    int
	every    time.Duration
	dropped  atomic.Uint64
	recorded atomic.Uint64
	failed   atomic.Uint64
}

// NewRecorder buffers up to buffer events for sink.
func NewRecorder(sink ports.EventSink, buffer int, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Recorder{ch: make(chan analytics.Event, max(buffer, 1)), sink: sink, log: log, batch: 500, every: time.Second}
}

// SetBatch tunes how many events are written together and how often a
// partial batch is flushed (defaults: 500 and 1s). Call before Run.
func (r *Recorder) SetBatch(n int, every time.Duration) {
	r.batch, r.every = max(n, 1), every
}

// Record queues e; it is safe on a nil Recorder (analytics disabled).
func (r *Recorder) Record(e analytics.Event) {
	if r == nil {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	select {
	case r.ch <- e:
	default:
		r.dropped.Add(1)
	}
}

// Dropped counts events lost to a full buffer (metric events_dropped_total).
func (r *Recorder) Dropped() uint64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// Recorded counts events written to the sink.
func (r *Recorder) Recorded() uint64 {
	if r == nil {
		return 0
	}
	return r.recorded.Load()
}

// Failed counts events lost because the sink returned an error.
func (r *Recorder) Failed() uint64 {
	if r == nil {
		return 0
	}
	return r.failed.Load()
}

// Run writes batches until ctx ends, then flushes what is left.
func (r *Recorder) Run(ctx context.Context) {
	t := time.NewTicker(r.every)
	defer t.Stop()
	buf := make([]analytics.Event, 0, r.batch)
	flush := func(ctx context.Context) {
		if len(buf) == 0 {
			return
		}
		if err := r.sink.InsertEvents(ctx, buf); err != nil {
			r.failed.Add(uint64(len(buf)))
			r.log.Error("analytics: insert events", "n", len(buf), "err", err)
		} else {
			r.recorded.Add(uint64(len(buf)))
		}
		buf = buf[:0]
	}
	for {
		select {
		case e := <-r.ch:
			if buf = append(buf, e); len(buf) >= r.batch {
				flush(ctx)
			}
		case <-t.C:
			flush(ctx)
		case <-ctx.Done():
			for {
				select {
				case e := <-r.ch:
					buf = append(buf, e)
				default:
					shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					flush(shutdown)
					cancel()
					return
				}
			}
		}
	}
}

// Dashboard answers the admin dashboard and runs analytics maintenance.
type Dashboard struct {
	store    ports.AnalyticsStore
	slang    ports.SlangStore
	recorder *Recorder
	now      func() time.Time
}

// NewDashboard wires the dashboard; slang is used by AutoBump.
func NewDashboard(store ports.AnalyticsStore, slang ports.SlangStore, recorder *Recorder) *Dashboard {
	return &Dashboard{store: store, slang: slang, recorder: recorder, now: time.Now}
}

// ParseWindow reads "7d", "30d", "24h"… (default 7 days, at most a year).
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return 7 * 24 * time.Hour, nil
	}
	var d time.Duration
	if n, err := strconv.Atoi(s[:len(s)-1]); err == nil && s[len(s)-1] == 'd' {
		d = time.Duration(n) * 24 * time.Hour
	} else if d, err = time.ParseDuration(s); err != nil {
		return 0, fmt.Errorf("%w: window %q", ErrInvalidParam, s)
	}
	if d <= 0 || d > 366*24*time.Hour {
		return 0, fmt.Errorf("%w: window %q", ErrInvalidParam, s)
	}
	return d, nil
}

// Overview is everything the dashboard page shows.
type Overview struct {
	Summary   analytics.Summary
	Daily     []analytics.Day
	Top       []analytics.QueryCount
	Rising    []analytics.RisingQuery
	Zero      []analytics.QueryCount
	Breakdown []analytics.BreakdownRow
	SlangRate float64 // share of clicks and saves that are on slang
	Slang     []analytics.SlangStat
	Window    time.Duration
}

// Summary is the headline for [since, until), plus this server's counters.
func (d *Dashboard) Summary(ctx context.Context, since, until time.Time) (analytics.Summary, error) {
	s, err := d.store.Summary(ctx, since, until)
	s.EventsDropped, s.EventsRecorded = d.recorder.Dropped(), d.recorder.Recorded()
	return s, err
}

// Overview gathers the whole dashboard for the last window.
func (d *Dashboard) Overview(ctx context.Context, window time.Duration) (*Overview, error) {
	until := d.now()
	since := until.Add(-window)
	o := &Overview{Window: window}
	var err error
	if o.Summary, err = d.Summary(ctx, since, until); err != nil {
		return nil, err
	}
	if o.Daily, err = d.store.Daily(ctx, since.Truncate(24*time.Hour), until); err != nil {
		return nil, err
	}
	if o.Top, err = d.store.TopQueries(ctx, window, 15); err != nil {
		return nil, err
	}
	if o.Rising, err = d.store.RisingQueries(ctx, 15); err != nil {
		return nil, err
	}
	if o.Zero, err = d.store.ZeroResultQueries(ctx, window, 15); err != nil {
		return nil, err
	}
	if o.Breakdown, err = d.store.Breakdown(ctx, window); err != nil {
		return nil, err
	}
	slangN, total, err := d.store.SlangRate(ctx, window)
	if err != nil {
		return nil, err
	}
	if total > 0 {
		o.SlangRate = float64(slangN) / float64(total)
	}
	o.Slang, err = d.store.TopSlang(ctx, window, 15)
	return o, err
}

// Users is the admin user table.
func (d *Dashboard) Users(ctx context.Context, sort string, limit int) ([]analytics.UserRow, error) {
	switch sort {
	case "", "active", "new", "searches":
	default:
		return nil, fmt.Errorf("%w: sort must be active, new or searches", ErrInvalidParam)
	}
	return d.store.Users(ctx, sort, min(max(limit, 1), 1000))
}

// ExportCSV is daily activity for [since, until) as CSV.
func (d *Dashboard) ExportCSV(ctx context.Context, since, until time.Time) ([]byte, error) {
	days, err := d.store.Daily(ctx, since, until)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"day", "actives", "searches", "saves"})
	for _, x := range days {
		_ = w.Write([]string{x.Day.Format("2006-01-02"), strconv.FormatInt(x.Actives, 10),
			strconv.FormatInt(x.Searches, 10), strconv.FormatInt(x.Saves, 10)})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// AutoBumpDelta is the weekly trend boost for slang whose save rate is in
// the top 10% (spec addendum §6).
const AutoBumpDelta = 0.2

// AutoBump raises the trend of the most-saved slang; run it weekly.
func (d *Dashboard) AutoBump(ctx context.Context) (int, error) {
	ids, err := d.store.SlangToBump(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := d.slang.BumpTrend(ctx, id, AutoBumpDelta); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		err = d.slang.NotifyIndex(ctx)
	}
	return len(ids), err
}

// Maintain refreshes the views and the event partitions (retention 180 days).
func (d *Dashboard) Maintain(ctx context.Context) error {
	if _, _, err := d.store.MaintainPartitions(ctx, 2, 180); err != nil {
		return err
	}
	return d.store.RefreshViews(ctx)
}
