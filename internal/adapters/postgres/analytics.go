package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"playword/internal/adapters/postgres/db"
	"playword/internal/domain/analytics"
	"playword/internal/ports"
)

var (
	_ ports.EventSink      = (*Store)(nil)
	_ ports.AnalyticsStore = (*Store)(nil)
)

func tstz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func optInt(n int) pgtype.Int4 { return pgtype.Int4{Int32: int32(n), Valid: n >= 0} }

func (s *Store) InsertEvents(ctx context.Context, events []analytics.Event) error {
	rows := make([]db.InsertEventsParams, len(events))
	for i, e := range events {
		props := []byte("{}")
		if len(e.Props) > 0 {
			props, _ = json.Marshal(e.Props)
		}
		var uid pgtype.UUID
		if e.UserID != "" {
			_ = uid.Scan(e.UserID)
		}
		rows[i] = db.InsertEventsParams{Ts: tstz(e.TS), UserID: uid, AnonID: nullable(e.AnonID), Type: e.Type,
			Lang: nullable(e.Lang), Dialect: nullable(e.Dialect), Level: nullable(e.Level), Query: nullable(e.Query),
			NSyllables:  pgtype.Int2{Int16: int16(e.NSyllables), Valid: e.NSyllables > 0},
			EntryID:     pgtype.Int8{Int64: e.EntryID, Valid: e.EntryID > 0},
			ResultCount: optInt(e.ResultCount), LatencyMs: optInt(e.LatencyMS), Props: props}
	}
	_, err := db.New(s.pool).InsertEvents(ctx, rows)
	return err
}

func (s *Store) Summary(ctx context.Context, since, until time.Time) (analytics.Summary, error) {
	r, err := db.New(s.pool).Summary(ctx, db.SummaryParams{DayFrom: tstz(until.Add(-24 * time.Hour)), Until: tstz(until),
		WeekFrom: tstz(until.Add(-7 * 24 * time.Hour)), MonthFrom: tstz(until.Add(-30 * 24 * time.Hour)), Since: tstz(since)})
	return analytics.Summary{DAU: r.Dau, WAU: r.Wau, MAU: r.Mau, Searches: r.Searches, Saves: r.Saves, NewUsers: r.NewUsers,
		P50Ms: r.P50Ms, P99Ms: r.P99Ms, Since: since, Until: until}, err
}

func (s *Store) Daily(ctx context.Context, since, until time.Time) ([]analytics.Day, error) {
	rows, err := db.New(s.pool).DailyActivity(ctx, db.DailyActivityParams{Since: tstz(since), Until: tstz(until)})
	out := make([]analytics.Day, len(rows))
	for i, r := range rows {
		out[i] = analytics.Day{Day: ts(r.Day), Actives: r.Actives, Searches: r.Searches, Saves: r.Saves}
	}
	return out, err
}

func (s *Store) TopQueries(ctx context.Context, window time.Duration, limit int) ([]analytics.QueryCount, error) {
	rows, err := db.New(s.pool).TopQueries(ctx, db.TopQueriesParams{WindowLen: window, Lim: int32(limit)})
	out := make([]analytics.QueryCount, len(rows))
	for i, r := range rows {
		out[i] = analytics.QueryCount{Lang: r.Lang, Query: r.Query.String, N: r.N}
	}
	return out, err
}

func (s *Store) RisingQueries(ctx context.Context, limit int) ([]analytics.RisingQuery, error) {
	rows, err := db.New(s.pool).RisingQueries(ctx, int32(limit))
	out := make([]analytics.RisingQuery, len(rows))
	for i, r := range rows {
		out[i] = analytics.RisingQuery{Lang: r.Lang, Query: r.Query.String, C7: r.C7, C30: r.C30, Rising: r.Rising}
	}
	return out, err
}

func (s *Store) ZeroResultQueries(ctx context.Context, window time.Duration, limit int) ([]analytics.QueryCount, error) {
	rows, err := db.New(s.pool).ZeroResultQueries(ctx, db.ZeroResultQueriesParams{WindowLen: window, Lim: int32(limit)})
	out := make([]analytics.QueryCount, len(rows))
	for i, r := range rows {
		out[i] = analytics.QueryCount{Lang: r.Lang, Query: r.Query.String, N: r.N}
	}
	return out, err
}

func (s *Store) Breakdown(ctx context.Context, window time.Duration) ([]analytics.BreakdownRow, error) {
	rows, err := db.New(s.pool).Breakdown(ctx, window)
	out := make([]analytics.BreakdownRow, len(rows))
	for i, r := range rows {
		out[i] = analytics.BreakdownRow{Dim: r.Dim, Value: r.Value, N: r.N}
	}
	return out, err
}

func (s *Store) SlangRate(ctx context.Context, window time.Duration) (int64, int64, error) {
	r, err := db.New(s.pool).SlangRate(ctx, window)
	return r.Slang, r.Total, err
}

func (s *Store) TopSlang(ctx context.Context, window time.Duration, limit int) ([]analytics.SlangStat, error) {
	rows, err := db.New(s.pool).TopSlang(ctx, db.TopSlangParams{WindowLen: window, Lim: int32(limit)})
	out := make([]analytics.SlangStat, len(rows))
	for i, r := range rows {
		out[i] = analytics.SlangStat{EntryID: r.ID, Headword: r.Headword, Trend: r.TrendScore, Views: r.Views, Saves: r.Saves}
	}
	return out, err
}

func (s *Store) SlangToBump(ctx context.Context) ([]int64, error) {
	return db.New(s.pool).SlangToBump(ctx)
}

func (s *Store) Users(ctx context.Context, sort string, limit int) ([]analytics.UserRow, error) {
	rows, err := db.New(s.pool).UserActivity(ctx, db.UserActivityParams{Sort: sort, Lim: int32(limit)})
	out := make([]analytics.UserRow, len(rows))
	for i, r := range rows {
		out[i] = analytics.UserRow{ID: r.ID, Email: r.Email, DisplayName: r.DisplayName, Role: r.Role,
			CreatedAt: ts(r.CreatedAt), LastSeen: ts(r.LastSeenAt), Searches: r.Searches, Books: r.Books, Items: r.Items}
	}
	return out, err
}

// RefreshViews refreshes the dashboard's materialized views without
// blocking readers (spec: every 10 minutes).
func (s *Store) RefreshViews(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `refresh materialized view concurrently mv_daily_activity;
		refresh materialized view concurrently mv_query_trends`)
	return err
}

// MaintainPartitions creates upcoming monthly partitions and drops expired
// ones (ADR-003 D8: retention 180 days).
func (s *Store) MaintainPartitions(ctx context.Context, monthsAhead, keepDays int) (int, int, error) {
	var made, dropped int
	if err := s.pool.QueryRow(ctx, "select playword_ensure_event_partitions($1)", monthsAhead).Scan(&made); err != nil {
		return 0, 0, err
	}
	err := s.pool.QueryRow(ctx, "select playword_drop_old_event_partitions($1)", keepDays).Scan(&dropped)
	return made, dropped, err
}

// ClaimJob reports whether a periodic job is due (its interval has passed
// since the last recorded run) and records this run if so.
func (s *Store) ClaimJob(ctx context.Context, name string, every time.Duration) (bool, error) {
	ok, err := db.New(s.pool).ClaimJob(ctx, db.ClaimJobParams{Name: name, Every: every})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}
