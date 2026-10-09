package httpadapter

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"playword/internal/app"
	"playword/internal/domain/analytics"
	"playword/internal/domain/entry"
)

const anonCookie = "pw_anon"

// actor fills who did it: the user, or the guest's random cookie.
func actor(r *http.Request, e *analytics.Event) {
	if u := currentUser(r); u != nil {
		e.UserID = u.ID
		return
	}
	if c, err := r.Cookie(anonCookie); err == nil {
		e.AnonID = c.Value
	}
}

// record queues an event; it never blocks the request.
func (s *Server) record(r *http.Request, e analytics.Event) {
	if s.opts.Recorder == nil {
		return
	}
	actor(r, &e)
	if e.ResultCount == 0 && e.Type != analytics.Search {
		e.ResultCount = -1
	}
	if e.LatencyMS == 0 && e.Type != analytics.Search {
		e.LatencyMS = -1
	}
	s.opts.Recorder.Record(e)
}

// queryText normalizes what was searched for the query tables (bounded).
func queryText(q string) string {
	q = entry.Normalize(q)
	if utf8.RuneCountInString(q) > 100 {
		q = string([]rune(q)[:100])
	}
	return q
}

// recordSearch logs a first-page rhyme search (spec: search events carry
// language, dialect, level, size, result count and latency).
func (s *Server) recordSearch(r *http.Request, p app.RhymeParams, res *app.RhymeResult, start time.Time) {
	if p.Text == "" || p.Cursor != "" || p.ExtraOver != "" {
		return
	}
	e := analytics.Event{Type: analytics.Search, Lang: string(p.Lang), Level: p.Level.String(), Query: queryText(p.Text),
		LatencyMS: int(time.Since(start).Milliseconds())}
	if p.Lang != entry.EN {
		e.Dialect = string(p.Dialect)
	}
	if res != nil {
		e.ResultCount = res.Total
		e.NSyllables = max(len(res.Query), len(res.QuerySegs))
	} else {
		e.NSyllables = len(strings.Fields(p.Text))
	}
	s.record(r, e)
}
