package httpadapter

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"playword/internal/adapters/http/views"
	"playword/internal/domain/analytics"
	"playword/internal/domain/rhyme"
)

type checkBody struct {
	Text    string `json:"text"`
	Dialect string `json:"dialect"`
	Level   string `json:"level"`
	Record  bool   `json:"record"` // count this check (the desk sends it once in a while, not per keystroke)
}

type checkTokenJSON struct {
	Start  int  `json:"s"`
	End    int  `json:"e"`
	Group  int  `json:"g,omitempty"` // index into groups, 1-based; 0: none
	Repeat bool `json:"r,omitempty"`
}

type checkGroupJSON struct {
	Letter  string   `json:"letter"`
	Level   string   `json:"level"`
	Label   string   `json:"label"`
	Members []string `json:"members"`
	Chains  int      `json:"chains"`
	Slot    int      `json:"slot"`      // fill 1…7 (views.CheckSlots)
	Under   int      `json:"underline"` // 0 none, 1 dashed, 2 dotted
}

type checkJSON struct {
	Tokens  []checkTokenJSON `json:"tokens"` // only words that are coloured or repeated
	Groups  []checkGroupJSON `json:"groups"`
	Lines   []string         `json:"lines"` // scheme letter of each line ("" when none)
	Schemes []string         `json:"schemes"`
	Density float64          `json:"density"`
	Rhymed  float64          `json:"rhymed"`
}

// checkParams reads dialect and level, defaulting to the user's settings.
func checkParams(r *http.Request, dialect, level string) (rhyme.Dialect, rhyme.Level, error) {
	v := url.Values{}
	if dialect != "" {
		v.Set("dialect", dialect)
	}
	if level != "" {
		v.Set("level", level)
	}
	p, err := rhymeParamsFor(pageQuery(v), currentUser(r))
	return p.Dialect, p.Level, err
}

func (s *Server) apiRhymeCheck(w http.ResponseWriter, r *http.Request) {
	var body checkBody
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	d, l, err := checkParams(r, body.Dialect, body.Level)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	res, err := s.svc.RhymeCheck(body.Text, d, l)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if body.Record {
		s.recordCheck(r, res, d, l)
	}
	out := checkJSON{Tokens: []checkTokenJSON{}, Groups: []checkGroupJSON{}, Schemes: res.Schemes(), Density: res.Density, Rhymed: res.Rhymed}
	index := map[int]int{}
	styles := views.CheckStyles(res)
	for i, g := range res.Groups {
		index[g.ID] = i + 1
		out.Groups = append(out.Groups, checkGroupJSON{Letter: g.Letter, Level: g.Level.String(), Label: g.Label, Members: g.Members,
			Chains: g.Chains, Slot: styles[i].Slot, Under: styles[i].Underline})
	}
	for _, t := range res.Tokens {
		if t.Group > 0 || t.Repeat {
			out.Tokens = append(out.Tokens, checkTokenJSON{Start: t.Start, End: t.End, Group: index[t.Group], Repeat: t.Repeat})
		}
	}
	for _, ln := range res.Lines {
		out.Lines = append(out.Lines, ln.Scheme)
	}
	writeJSON(w, http.StatusOK, out)
}

// recordCheck counts a rhyme check with its scheme: what was rhymed, never
// the words.
func (s *Server) recordCheck(r *http.Request, res rhyme.CheckResult, d rhyme.Dialect, l rhyme.Level) {
	if !s.checkThrottle.allow(actorKey(r), time.Minute) {
		return // at most one rhyme check event a minute per person
	}
	lines := 0
	for _, ln := range res.Lines {
		if ln.Stanza >= 0 {
			lines++
		}
	}
	s.record(r, analytics.Event{Type: analytics.RhymeCheck, Lang: "vi", Dialect: string(d), Level: l.String(),
		Props: map[string]any{"schemes": res.Schemes(), "groups": len(res.Groups), "lines": lines,
			"density": round2(res.Density), "rhymed": round2(res.Rhymed)}})
}

func round2(x float64) float64 { return float64(int(x*100+0.5)) / 100 }

// pageCheck is the coloured, read-only lyrics: the writing desk's "Kiểm tra
// vần" without JavaScript.
func (s *Server) pageCheck(w http.ResponseWriter, r *http.Request) {
	text := r.PostFormValue("lyrics")
	d, l, err := checkParams(r, r.PostFormValue("dialect"), r.PostFormValue("level"))
	if err == nil {
		var res rhyme.CheckResult
		if res, err = s.svc.RhymeCheck(text, d, l); err == nil {
			s.recordCheck(r, res, d, l)
			render(w, r, http.StatusOK, views.CheckPage(views.CheckModel{Page: s.page(r), Text: text, Result: res,
				Dialect: d, Level: l, Back: safeNext(r.PostFormValue("next"))}))
			return
		}
	}
	status, _, _ := classify(err)
	render(w, r, status, views.CheckPage(views.CheckModel{Page: s.page(r), Text: text, Error: userMessage(err), Back: safeNext(r.PostFormValue("next"))}))
}

// pageBookCheck colours a book's lyrics, for whoever may read the book.
func (s *Server) pageBookCheck(w http.ResponseWriter, r *http.Request) {
	b, _, err := s.accounts.Book(r.Context(), currentUser(r), r.PathValue("id"))
	if err != nil {
		s.renderBook(w, r, r.PathValue("id"), accountMessage(err), statusOf(err))
		return
	}
	d, l, _ := checkParams(r, r.URL.Query().Get("dialect"), r.URL.Query().Get("level"))
	res, err := s.svc.RhymeCheck(b.Lyrics, d, l)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	render(w, r, http.StatusOK, views.CheckPage(views.CheckModel{Page: s.page(r), Title: b.Title, Text: b.Lyrics, Result: res,
		Dialect: d, Level: l, Back: "/books/" + b.ID}))
}

// throttle lets one event through per key and period (in memory; a
// restart forgets, which only lets one extra event through).
type throttle struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (t *throttle) allow(key string, every time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.last == nil || len(t.last) > 100000 {
		t.last = map[string]time.Time{}
	}
	if now.Sub(t.last[key]) < every {
		return false
	}
	t.last[key] = now
	return true
}

// actorKey is who a request is from: the user, else the guest cookie, else the IP.
func actorKey(r *http.Request) string {
	if u := currentUser(r); u != nil {
		return "u:" + u.ID
	}
	if c, err := r.Cookie(anonCookie); err == nil && c.Value != "" {
		return "a:" + c.Value
	}
	return "ip:" + r.RemoteAddr
}
