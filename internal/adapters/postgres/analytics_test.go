package postgres_test

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"playword/internal/app"
	"playword/internal/domain/analytics"
	"playword/internal/domain/entry"
	"playword/internal/ports"
)

// fakeSrc is enough of a ports.Source to create entries through ingest.
func TestAnalytics(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	now := time.Now()
	day := 24 * time.Hour

	user, err := st.CreateUser(ctx, "fan@example.vn", "h", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(1, 1))
	var ev []analytics.Event
	add := func(n int, q string, from, to time.Duration, results int) {
		for range n {
			at := now.Add(-from - time.Duration(r.Int64N(int64(to-from))))
			ev = append(ev, analytics.Event{TS: at, AnonID: "a" + string(rune('a'+r.IntN(20))), Type: analytics.Search,
				Lang: "vi", Dialect: "bac", Level: "L25", Query: q, NSyllables: 2, ResultCount: results, LatencyMS: 2 + r.IntN(10)})
		}
	}
	add(30, "ngày tàn", time.Hour, 6*day, 40) // rising: 30 this week…
	add(8, "ngày tàn", 8*day, 36*day, 40)     // …vs ~1.9 per week before
	add(6, "cũ rích", time.Hour, 6*day, 12)   // falling
	add(60, "cũ rích", 8*day, 36*day, 12)
	add(7, "flex", time.Hour, 6*day, 0)      // zero results: a slang to add
	add(9800, "nhiễu", time.Hour, 60*day, 3) // noise up to 10k events
	ev = append(ev,
		analytics.Event{TS: now.Add(-time.Hour), UserID: user.ID, Type: analytics.Search, Lang: "en", Level: "family", Query: "money", NSyllables: 2, ResultCount: 9, LatencyMS: 1},
		analytics.Event{TS: now.Add(-time.Hour), UserID: user.ID, Type: analytics.Login, ResultCount: -1, LatencyMS: -1},
	)
	if len(ev) != 9913 {
		t.Fatalf("built %d events", len(ev))
	}
	if err := st.InsertEvents(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if err := st.RefreshViews(ctx); err != nil {
		t.Fatal(err)
	}

	// New queries (no history) lead as "new"; then the biggest risers.
	rising, err := st.RisingQueries(ctx, 10)
	if err != nil || len(rising) < 2 || rising[0].Query != "flex" || rising[0].Rising != 999 ||
		rising[1].Query != "ngày tàn" || rising[1].C7 != 30 || rising[1].C30 != 8 {
		t.Fatalf("rising = %+v %v", rising, err)
	}
	for _, q := range rising {
		if q.Query == "cũ rích" && q.Rising >= 1 {
			t.Errorf("cũ rích is falling: %+v", q)
		}
	}
	zero, _ := st.ZeroResultQueries(ctx, 7*day, 10)
	if len(zero) != 1 || zero[0].Query != "flex" || zero[0].N != 7 {
		t.Errorf("zero = %+v", zero)
	}
	top, _ := st.TopQueries(ctx, 7*day, 3)
	if len(top) == 0 || top[0].Query != "nhiễu" {
		t.Errorf("top = %+v", top)
	}
	sum, err := st.Summary(ctx, now.Add(-7*day), now.Add(time.Minute))
	if err != nil || sum.MAU < 20 || sum.Searches < 1000 || sum.P50Ms < 2 || sum.P99Ms < sum.P50Ms {
		t.Errorf("summary = %+v %v", sum, err)
	}
	days, _ := st.Daily(ctx, now.Add(-10*day), now.Add(day))
	if len(days) < 9 {
		t.Errorf("daily rows = %d", len(days))
	}
	bd, _ := st.Breakdown(ctx, 7*day)
	langs := map[string]int64{}
	for _, b := range bd {
		if b.Dim == "lang" {
			langs[b.Value] = b.N
		}
	}
	if langs["en"] != 1 || langs["vi"] < 1000 {
		t.Errorf("breakdown = %v", langs)
	}
	users, _ := st.Users(ctx, "searches", 10)
	if len(users) != 1 || users[0].Searches != 1 {
		t.Errorf("users = %+v", users)
	}

	// Export goes through the dashboard service.
	dash := app.NewDashboard(st, st, nil)
	csv, err := dash.ExportCSV(ctx, now.Add(-3*day), now.Add(day))
	if err != nil || len(csv) < 30 {
		t.Errorf("csv = %q %v", csv, err)
	}
	if n, err := dash.AutoBump(ctx); err != nil || n != 0 {
		t.Errorf("no slang events, nothing to bump: %d %v", n, err)
	}

	// Retention: an event older than 180 days in the default partition goes.
	if err := st.InsertEvents(ctx, []analytics.Event{{TS: now.Add(-400 * day), Type: analytics.Login, ResultCount: -1, LatencyMS: -1}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MaintainPartitions(ctx, 2, 180); err != nil {
		t.Fatal(err)
	}
	if err := dash.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	var old int
	if err := st.Pool().QueryRow(ctx, "select count(*) from events where ts < now() - interval '180 days'").Scan(&old); err != nil || old != 0 {
		t.Errorf("old events = %d %v", old, err)
	}

	// Deleting the account anonymizes its events (spec: GDPR-style).
	if err := st.DeleteUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = st.Pool().QueryRow(ctx, "select count(*) from events where user_id is not null").Scan(&left)
	var anon int
	_ = st.Pool().QueryRow(ctx, "select count(*) from events where anon_id like 'deleted:%'").Scan(&anon)
	if left != 0 || anon != 2 {
		t.Errorf("after delete: %d with user, %d anonymized", left, anon)
	}

	if due, _ := st.ClaimJob(ctx, "weekly", 7*day); !due {
		t.Error("first claim runs")
	}
	if due, _ := st.ClaimJob(ctx, "weekly", 7*day); due {
		t.Error("a second claim within the week does not")
	}
	var _ ports.AnalyticsStore = st
	_ = entry.VI
}
