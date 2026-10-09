package ports

import (
	"context"
	"time"

	"playword/internal/domain/analytics"
)

// EventSink stores batches of events.
type EventSink interface {
	InsertEvents(ctx context.Context, events []analytics.Event) error
}

// AnalyticsStore answers the dashboard (spec addendum §6) and runs its
// maintenance.
type AnalyticsStore interface {
	Summary(ctx context.Context, since, until time.Time) (analytics.Summary, error)
	Daily(ctx context.Context, since, until time.Time) ([]analytics.Day, error)
	TopQueries(ctx context.Context, window time.Duration, limit int) ([]analytics.QueryCount, error)
	RisingQueries(ctx context.Context, limit int) ([]analytics.RisingQuery, error)
	ZeroResultQueries(ctx context.Context, window time.Duration, limit int) ([]analytics.QueryCount, error)
	Breakdown(ctx context.Context, window time.Duration) ([]analytics.BreakdownRow, error)
	SlangRate(ctx context.Context, window time.Duration) (slang, total int64, err error)
	TopSlang(ctx context.Context, window time.Duration, limit int) ([]analytics.SlangStat, error)
	SlangToBump(ctx context.Context) ([]int64, error)
	Users(ctx context.Context, sort string, limit int) ([]analytics.UserRow, error)
	RefreshViews(ctx context.Context) error
	MaintainPartitions(ctx context.Context, monthsAhead, keepDays int) (made, dropped int, err error)
}
