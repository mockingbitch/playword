package httpadapter

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"playword/internal/adapters/http/views"
	"playword/internal/app"
	"playword/internal/domain/account"
	"playword/internal/domain/analytics"
	"playword/internal/domain/entry"
	"playword/internal/domain/slang"
)

const adminTimeout = 30 * time.Second // spec §5: 30s for admin

const adminTokenKey ctxKey = 100

// bearerAdmin reports a request authenticated by PLAYWORD_ADMIN_TOKEN (spec
// §6: "auth bằng ADMIN_TOKEN giai đoạn đầu"); such requests carry no cookie
// credentials, so they skip the CSRF check.
func (s *Server) bearerAdmin(r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.opts.AdminToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.AdminToken)) == 1
}

func (s *Server) isAdmin(r *http.Request) bool {
	if v, _ := r.Context().Value(adminTokenKey).(bool); v {
		return true
	}
	return currentUser(r).IsAdmin()
}

// admin wraps admin routes: 404 for everyone else, so the routes do not
// advertise themselves; a 30-second timeout.
func (s *Server) admin(h http.HandlerFunc) http.Handler {
	return withTimeout(adminTimeout, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAdmin(r) {
			if currentUser(r) == nil && !strings.HasPrefix(r.URL.Path, "/api/") {
				s.requireUser(w, r)
				return
			}
			writeProblemStatus(w, r, http.StatusNotFound, "not-found", "Không tìm thấy", "")
			return
		}
		h(w, r)
	}))
}

func reviewer(r *http.Request) string {
	if u := currentUser(r); u != nil {
		return u.Email
	}
	return "admin-token"
}

func slangMessage(err error) string {
	switch {
	case errors.Is(err, slang.ErrInvalid):
		return "Chưa hợp lệ: " + strings.TrimPrefix(err.Error(), slang.ErrInvalid.Error()+": ")
	case errors.Is(err, account.ErrNotFound):
		return "Không tìm thấy."
	}
	return "Có lỗi, thử lại sau."
}

func slangStatus(err error) int {
	switch {
	case errors.Is(err, slang.ErrInvalid):
		return http.StatusUnprocessableEntity
	case errors.Is(err, account.ErrNotFound), errors.Is(err, app.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, app.ErrBusy):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func (s *Server) submitter(r *http.Request) string {
	if u := currentUser(r); u != nil {
		return u.Email
	}
	return "guest:" + account.HashIP(clientIP(r, s.opts.TrustProxy), "slang")
}

func (s *Server) pageSlangNew(w http.ResponseWriter, r *http.Request) {
	m := views.SlangModel{Page: s.page(r), Sub: slang.Submission{Lang: entry.VI, Headword: r.URL.Query().Get("headword")}}
	if r.Method == http.MethodPost {
		m.Sub = slang.Submission{Headword: r.PostFormValue("headword"), Lang: entry.Lang(r.PostFormValue("lang")),
			Gloss: r.PostFormValue("gloss"), Example: r.PostFormValue("example"), Respelling: r.PostFormValue("respelling"),
			SubmittedBy: s.submitter(r)}
		if err := s.slang.Submit(r.Context(), &m.Sub); err != nil {
			m.Error = slangMessage(err)
			render(w, r, slangStatus(err), views.SlangNewPage(m))
			return
		}
		s.record(r, analytics.Event{Type: analytics.SubmitSlang, Lang: string(m.Sub.Lang), Query: queryText(m.Sub.Headword)})
		m.Message, m.Sub = "Cảm ơn! “"+m.Sub.Headword+"” đang chờ duyệt.", slang.Submission{Lang: m.Sub.Lang}
	}
	render(w, r, http.StatusOK, views.SlangNewPage(m))
}

func (s *Server) pageAdminSlang(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = slang.Pending
	}
	m := views.AdminSlangModel{Page: s.page(r), Status: status}
	subs, err := s.slang.List(r.Context(), status, 200)
	if err != nil {
		m.Error = slangMessage(err)
	}
	m.Subs = subs
	render(w, r, http.StatusOK, views.AdminSlangPage(m))
}

func subID(r *http.Request) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, account.ErrNotFound
	}
	return n, nil
}

// pageAdminReview approves or rejects from the admin list (HTMX: swaps the row).
func (s *Server) pageAdminReview(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := subID(r)
		var sub *slang.Submission
		if err == nil {
			if approve {
				sub, err = s.slang.Approve(r.Context(), reviewer(r), id)
			} else if err = s.slang.Reject(r.Context(), reviewer(r), id); err == nil {
				subs, _ := s.slang.List(r.Context(), slang.Rejected, 500)
				for i := range subs {
					if subs[i].ID == id {
						sub = &subs[i]
					}
				}
			}
		}
		if err != nil {
			render(w, r, http.StatusOK, views.AdminSlangRow(slang.Submission{ID: id, Headword: "#" + strconv.FormatInt(id, 10)}, slangMessage(err)))
			return
		}
		if sub == nil {
			sub = &slang.Submission{ID: id}
		}
		render(w, r, http.StatusOK, views.AdminSlangRow(*sub, ""))
	}
}

type submissionJSON struct {
	ID          int64     `json:"id"`
	Headword    string    `json:"headword"`
	Lang        string    `json:"lang"`
	Gloss       string    `json:"gloss"`
	Example     string    `json:"example,omitempty"`
	Respelling  string    `json:"respelling,omitempty"`
	Tags        []string  `json:"tags"`
	SubmittedBy string    `json:"submitted_by,omitempty"`
	Status      string    `json:"status"`
	EntryID     int64     `json:"entry_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func toSubmissionJSON(s slang.Submission) submissionJSON {
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	return submissionJSON{s.ID, s.Headword, string(s.Lang), s.Gloss, s.Example, s.Respelling, tags, s.SubmittedBy, s.Status, s.EntryID, s.CreatedAt}
}

func writeSlangProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := slangStatus(err)
	if status == http.StatusInternalServerError {
		writeProblem(w, r, err)
		return
	}
	writeProblemStatus(w, r, status, "slang", slangMessage(err), err.Error())
}

func (s *Server) apiSubmitSlang(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Headword   string   `json:"headword"`
		Lang       string   `json:"lang"`
		Gloss      string   `json:"gloss"`
		Example    string   `json:"example"`
		Respelling string   `json:"respelling"`
		Tags       []string `json:"tags"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeSlangProblem(w, r, slang.ErrInvalid)
		return
	}
	sub := slang.Submission{Headword: body.Headword, Lang: entry.Lang(body.Lang), Gloss: body.Gloss, Example: body.Example,
		Respelling: body.Respelling, Tags: body.Tags, SubmittedBy: s.submitter(r)}
	if err := s.slang.Submit(r.Context(), &sub); err != nil {
		writeSlangProblem(w, r, err)
		return
	}
	s.record(r, analytics.Event{Type: analytics.SubmitSlang, Lang: string(sub.Lang), Query: queryText(sub.Headword)})
	writeJSON(w, http.StatusCreated, toSubmissionJSON(sub))
}

func (s *Server) apiAdminSlang(w http.ResponseWriter, r *http.Request) {
	subs, err := s.slang.List(r.Context(), r.URL.Query().Get("status"), 500)
	if err != nil {
		writeSlangProblem(w, r, err)
		return
	}
	out := make([]submissionJSON, len(subs))
	for i, sub := range subs {
		out[i] = toSubmissionJSON(sub)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiAdminReview(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := subID(r)
		if err != nil {
			writeSlangProblem(w, r, err)
			return
		}
		if !approve {
			if err := s.slang.Reject(r.Context(), reviewer(r), id); err != nil {
				writeSlangProblem(w, r, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		sub, err := s.slang.Approve(r.Context(), reviewer(r), id)
		if err != nil {
			writeSlangProblem(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toSubmissionJSON(*sub))
	}
}

func (s *Server) apiAdminTrend(w http.ResponseWriter, r *http.Request) {
	id, err := subID(r)
	var body struct {
		Delta float64 `json:"delta"`
	}
	if err == nil {
		err = decodeJSON(r, &body)
	}
	if err != nil {
		writeSlangProblem(w, r, slang.ErrInvalid)
		return
	}
	v, err := s.slang.Bump(r.Context(), id, body.Delta)
	if err != nil {
		writeSlangProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]float64{"trend_score": v})
}

// apiAdminIngest starts an ingest in the background (spec §6) and answers
// 202; progress is in ingest_runs.
func (s *Server) apiAdminIngest(w http.ResponseWriter, r *http.Request) {
	if s.opts.Ingest == nil {
		writeSlangProblem(w, r, app.ErrNotFound)
		return
	}
	name := r.PathValue("source")
	dry := r.URL.Query().Get("dry_run") == "true"
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Hour)
		defer cancel()
		rep, err := s.opts.Ingest.Run(ctx, name, dry)
		if err != nil {
			s.opts.Log.Error("admin ingest", "source", name, "err", err)
			return
		}
		s.opts.Log.Info("admin ingest finished", "source", name, "status", rep.Status, "inserted", rep.Inserted, "updated", rep.Updated)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"source": name, "dry_run": dry, "status": "started"})
}
