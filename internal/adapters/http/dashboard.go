package httpadapter

import (
	"net/http"
	"strconv"
	"time"

	"playword/internal/adapters/http/views"
	"playword/internal/app"
)

func (s *Server) pageDashboard(w http.ResponseWriter, r *http.Request) {
	win := r.URL.Query().Get("window")
	if win == "" {
		win = "7d"
	}
	m := views.DashboardModel{Page: s.page(r), Window: win}
	d, err := app.ParseWindow(win)
	if err == nil {
		m.O, err = s.opts.Dashboard.Overview(r.Context(), d)
	}
	if err != nil {
		m.Error = userMessage(err)
	}
	render(w, r, http.StatusOK, views.DashboardPage(m))
}

func (s *Server) pageUsers(w http.ResponseWriter, r *http.Request) {
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "active"
	}
	users, err := s.opts.Dashboard.Users(r.Context(), sort, 200)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	render(w, r, http.StatusOK, views.UsersPage(views.UsersModel{Page: s.page(r), Users: users, Sort: sort}))
}

// timeRange reads ?from&to (RFC 3339 or YYYY-MM-DD) or ?window=7d.
func timeRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	parse := func(v string) (time.Time, error) {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return t, app.ErrInvalidParam
		}
		return t, nil
	}
	until := time.Now()
	if v := q.Get("to"); v != "" {
		t, err := parse(v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		until = t
	}
	if v := q.Get("from"); v != "" {
		since, err := parse(v)
		if err != nil || !since.Before(until) {
			return time.Time{}, time.Time{}, app.ErrInvalidParam
		}
		return since, until, nil
	}
	d, err := app.ParseWindow(q.Get("window"))
	return until.Add(-d), until, err
}

func (s *Server) apiDashSummary(w http.ResponseWriter, r *http.Request) {
	since, until, err := timeRange(r)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	sum, err := s.opts.Dashboard.Summary(r.Context(), since, until)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": since, "to": until, "dau": sum.DAU, "wau": sum.WAU, "mau": sum.MAU,
		"searches": sum.Searches, "saves": sum.Saves, "new_users": sum.NewUsers, "p50_ms": sum.P50Ms, "p99_ms": sum.P99Ms,
		"events_dropped": sum.EventsDropped, "events_recorded": sum.EventsRecorded})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) *app.Overview {
	d, err := app.ParseWindow(r.URL.Query().Get("window"))
	if err != nil {
		writeProblem(w, r, err)
		return nil
	}
	o, err := s.opts.Dashboard.Overview(r.Context(), d)
	if err != nil {
		writeProblem(w, r, err)
		return nil
	}
	return o
}

func (s *Server) apiDashQueries(w http.ResponseWriter, r *http.Request) {
	if o := s.overview(w, r); o != nil {
		writeJSON(w, http.StatusOK, map[string]any{"window_days": o.Window.Hours() / 24, "top": nonNil(o.Top),
			"rising": nonNil(o.Rising), "zero_result": nonNil(o.Zero)})
	}
}

func (s *Server) apiDashBreakdown(w http.ResponseWriter, r *http.Request) {
	if o := s.overview(w, r); o != nil {
		by := map[string]map[string]int64{}
		for _, b := range o.Breakdown {
			if by[b.Dim] == nil {
				by[b.Dim] = map[string]int64{}
			}
			by[b.Dim][b.Value] = b.N
		}
		writeJSON(w, http.StatusOK, map[string]any{"window_days": o.Window.Hours() / 24, "by": by,
			"slang_click_rate": o.SlangRate, "top_slang": nonNil(o.Slang)})
	}
}

func (s *Server) apiDashUsers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 100
	}
	users, err := s.opts.Dashboard.Users(r.Context(), r.URL.Query().Get("sort"), limit)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(users))
}

func (s *Server) apiDashExport(w http.ResponseWriter, r *http.Request) {
	since, until, err := timeRange(r)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	data, err := s.opts.Dashboard.ExportCSV(r.Context(), since, until)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="playword-activity.csv"`)
	_, _ = w.Write(data)
}
