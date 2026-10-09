package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ParseSchedule reads PLAYWORD_INGEST_SCHEDULE: "source=interval,…", e.g.
// "hnd=168h,oewn=720h" (Go durations, at least one minute).
func ParseSchedule(s string) (map[string]time.Duration, error) {
	out := map[string]time.Duration{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		name, every, ok := strings.Cut(part, "=")
		d, err := time.ParseDuration(strings.TrimSpace(every))
		if !ok || err != nil || d < time.Minute || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("schedule %q: want source=duration (≥ 1m)", part)
		}
		out[strings.TrimSpace(name)] = d
	}
	return out, nil
}

// Schedule runs each source every interval until ctx ends. run is expected
// to take a cluster-wide lock, so several servers do not ingest twice.
func Schedule(ctx context.Context, every map[string]time.Duration, run func(context.Context, string) error, log *slog.Logger) {
	for name, d := range every {
		go func() {
			t := time.NewTicker(d)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := run(ctx, name); err != nil {
						log.Error("scheduled ingest failed", "source", name, "err", err)
					}
				}
			}
		}()
	}
}
