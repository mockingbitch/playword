package httpadapter

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"playword/internal/app"
	"playword/internal/domain/analytics"
)

type memSink struct {
	mu    sync.Mutex
	ev    []analytics.Event
	block chan struct{}
}

func (m *memSink) InsertEvents(_ context.Context, ev []analytics.Event) error {
	if m.block != nil {
		<-m.block
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ev = append(m.ev, ev...)
	return nil
}

func (m *memSink) types() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int{}
	for _, e := range m.ev {
		out[e.Type]++
	}
	return out
}

// stats is a canned AnalyticsStore.
type stats struct{}

func (stats) Summary(context.Context, time.Time, time.Time) (analytics.Summary, error) {
	return analytics.Summary{DAU: 3, WAU: 12, MAU: 40, Searches: 1284, P50Ms: 2, P99Ms: 9}, nil
}
func (stats) Daily(context.Context, time.Time, time.Time) ([]analytics.Day, error) {
	return []analytics.Day{{Day: time.Now().Add(-48 * time.Hour), Actives: 2, Searches: 10}, {Day: time.Now(), Actives: 3, Searches: 25}}, nil
}
func (stats) TopQueries(context.Context, time.Duration, int) ([]analytics.QueryCount, error) {
	return []analytics.QueryCount{{Lang: "vi", Query: "ngày tàn", N: 30}}, nil
}
func (stats) RisingQueries(context.Context, int) ([]analytics.RisingQuery, error) {
	return []analytics.RisingQuery{{Lang: "vi", Query: "ngày tàn", C7: 30, C30: 8, Rising: 16}}, nil
}
func (stats) ZeroResultQueries(context.Context, time.Duration, int) ([]analytics.QueryCount, error) {
	return []analytics.QueryCount{{Lang: "vi", Query: "flex", N: 7}}, nil
}
func (stats) Breakdown(context.Context, time.Duration) ([]analytics.BreakdownRow, error) {
	return []analytics.BreakdownRow{{Dim: "level", Value: "L25", N: 90}, {Dim: "level", Value: "L0", N: 10}, {Dim: "lang", Value: "vi", N: 99}}, nil
}
func (stats) SlangRate(context.Context, time.Duration) (int64, int64, error) { return 1, 4, nil }
func (stats) TopSlang(context.Context, time.Duration, int) ([]analytics.SlangStat, error) {
	return []analytics.SlangStat{{EntryID: 1, Headword: "chill", Trend: 1.2, Views: 10, Saves: 4}}, nil
}
func (stats) SlangToBump(context.Context) ([]int64, error) { return nil, nil }
func (stats) Users(context.Context, string, int) ([]analytics.UserRow, error) {
	return []analytics.UserRow{{Email: "mc@example.vn", Role: "admin", Searches: 5}}, nil
}
func (stats) RefreshViews(context.Context) error                             { return nil }
func (stats) MaintainPartitions(context.Context, int, int) (int, int, error) { return 0, 0, nil }

func TestEventsAndDashboard(t *testing.T) {
	sink := &memSink{}
	rec := app.NewRecorder(sink, 100, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { rec.Run(ctx); close(done) }()
	ts, _, _ := testServerWith(t, Options{Recorder: rec, Dashboard: app.NewDashboard(stats{}, nil, rec), AdminToken: adminToken})
	b := newBrowser(t, ts)

	b.do("GET", "/?q="+q("ngày tàn"), "", nil)
	b.do("GET", "/api/v1/rhymes?q=wifi", "", nil) // unparseable: a zero-result search
	b.do("GET", "/word/vi/"+url.PathEscape("may mắn")+"?from=rhyme&q=ngay", "", nil, "HX-Request", "true")
	b.do("GET", "/dictionary?q="+q("may mắn"), "", nil)
	b.form("/save", url.Values{"entry_id": {"1"}, "lang": {"vi"}, "matched_from": {"ngày tàn"}})
	b.form("/register", url.Values{"email": {"x@example.vn"}, "password": {"correct horse battery"}})
	cancel()
	<-done
	got := sink.types()
	want := map[string]int{"search": 2, "click_result": 1, "view_word": 1, "save_item": 1, "login": 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s events = %d, want %d (all %v)", k, got[k], n, got)
		}
	}
	for _, e := range sink.ev {
		if e.UserID == "" && e.AnonID == "" {
			t.Errorf("event without an actor: %+v", e)
		}
		if e.Type == "search" && e.Query == "wifi" && e.ResultCount != 0 {
			t.Errorf("wifi is a zero-result search: %+v", e)
		}
	}

	r, body := b.do("GET", "/admin/dashboard?window=30d", "", nil, "Authorization", "Bearer "+adminToken)
	for _, w := range []string{"Người ta đang tra gì nhiều lên", "ngày tàn", "×16.0", "flex", "Thêm slang", "<svg", `class="bar"`, "1,284", "chill", "40.0%"} {
		if !strings.Contains(body, w) {
			t.Errorf("dashboard missing %q", w)
		}
	}
	if r.StatusCode != 200 || !strings.Contains(body, `hx-trigger="every 60s"`) {
		t.Errorf("dashboard: %d", r.StatusCode)
	}
	for path, want := range map[string]string{
		"/api/v1/admin/dashboard/summary?window=7d":   `"mau":40`,
		"/api/v1/admin/dashboard/queries":             `"Query":"flex"`,
		"/api/v1/admin/dashboard/breakdown":           `"slang_click_rate":0.25`,
		"/api/v1/admin/dashboard/users?sort=searches": `mc@example.vn`,
		"/api/v1/admin/dashboard/export.csv":          "day,actives,searches,saves",
		"/admin/users":                                "mc@example.vn",
	} {
		if r, body := b.do("GET", path, "", nil, "Authorization", "Bearer "+adminToken); r.StatusCode != 200 || !strings.Contains(body, want) {
			t.Errorf("%s: %d %s", path, r.StatusCode, body)
		}
	}
	for _, bad := range []string{"/api/v1/admin/dashboard/summary?window=x", "/api/v1/admin/dashboard/summary?from=2026-10-09&to=2026-10-01", "/api/v1/admin/dashboard/users?sort=x"} {
		if r, _ := b.do("GET", bad, "", nil, "Authorization", "Bearer "+adminToken); r.StatusCode != 400 {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}
	if r, _ := b.do("GET", "/admin/dashboard", "", nil); r.StatusCode != 404 {
		t.Errorf("non-admin dashboard: %d", r.StatusCode)
	}
}

// Spec §7: a full event buffer drops events, counts them, and requests
// still succeed.
func TestFullEventBufferDoesNotHurtRequests(t *testing.T) {
	sink := &memSink{block: make(chan struct{})}
	defer close(sink.block)
	rec := app.NewRecorder(sink, 1, nil)
	rec.SetBatch(1, time.Millisecond) // the worker blocks on the stuck sink at once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rec.Run(ctx)
	ts, _, _ := testServerWith(t, Options{Recorder: rec})
	b := newBrowser(t, ts)
	for range 30 {
		if r, _ := b.do("GET", "/api/v1/rhymes?q="+q("ngày tàn"), "", nil); r.StatusCode != 200 {
			t.Fatalf("status %d", r.StatusCode)
		}
	}
	if rec.Dropped() == 0 {
		t.Error("events_dropped must grow")
	}
}
