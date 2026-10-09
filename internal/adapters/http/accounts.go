package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"playword/internal/adapters/http/views"
	"playword/internal/app"
	"playword/internal/domain/account"
	"playword/internal/domain/analytics"
	"playword/internal/domain/entry"
)

// page is the context every full page renders with.
func (s *Server) page(r *http.Request) views.Page {
	return views.Page{User: currentUser(r), CSRF: csrfToken(r)}
}

// accountMessage is the Vietnamese text for an account error.
func accountMessage(err error) string {
	switch {
	case errors.Is(err, account.ErrPasswordTooShort):
		return "Mật khẩu cần ít nhất 10 ký tự."
	case errors.Is(err, account.ErrEmailTaken):
		return "Email này đã có tài khoản."
	case errors.Is(err, account.ErrBadCredentials):
		return "Sai email hoặc mật khẩu."
	case errors.Is(err, account.ErrLocked):
		return "Tài khoản tạm khoá 15 phút do đăng nhập sai nhiều lần."
	case errors.Is(err, account.ErrInvalidInput):
		return "Thông tin chưa hợp lệ: " + strings.TrimPrefix(err.Error(), account.ErrInvalidInput.Error()+": ")
	case errors.Is(err, account.ErrNotFound):
		return "Không tìm thấy."
	case errors.Is(err, account.ErrUnauthenticated):
		return "Cần đăng nhập."
	case errors.Is(err, account.ErrEmailNotVerified):
		return "Email của tài khoản Google chưa được xác minh."
	}
	return "Có lỗi, thử lại sau."
}

func (s *Server) pageAuth(register bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := views.AuthModel{Page: s.page(r), Register: register, Next: safeNext(r.URL.Query().Get("next")), Providers: s.providers()}
		if r.Method == http.MethodGet {
			if m.Page.User != nil {
				http.Redirect(w, r, m.Next, http.StatusSeeOther)
				return
			}
			render(w, r, http.StatusOK, views.AuthPage(m))
			return
		}
		m.Email, m.Name, m.Next = r.PostFormValue("email"), r.PostFormValue("name"), safeNext(r.PostFormValue("next"))
		ip, ua := clientIP(r, s.opts.TrustProxy), r.UserAgent()
		var u *account.User
		var token string
		var err error
		if register {
			u, token, err = s.accounts.Register(r.Context(), m.Email, r.PostFormValue("password"), m.Name, ip, ua)
		} else {
			u, token, err = s.accounts.Login(r.Context(), m.Email, r.PostFormValue("password"), ip, ua)
		}
		if err != nil {
			m.Error = accountMessage(err)
			render(w, r, statusOf(err), views.AuthPage(m))
			return
		}
		s.setSession(w, token)
		s.record(r.WithContext(context.WithValue(r.Context(), userKey, u)), analytics.Event{Type: analytics.Login})
		if register {
			if items := guestItems(r); len(items) > 0 {
				if b, err := s.accounts.ImportGuest(r.Context(), u, items); err == nil && b != nil {
					s.setGuestItems(w, nil)
					m.Next = "/books/" + b.ID
				}
			}
		}
		http.Redirect(w, r, m.Next, http.StatusSeeOther)
	}
}

// statusOf maps account errors to HTTP statuses.
func statusOf(err error) int {
	switch {
	case errors.Is(err, account.ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, account.ErrEmailTaken):
		return http.StatusConflict
	case errors.Is(err, account.ErrBadCredentials), errors.Is(err, account.ErrUnauthenticated),
		errors.Is(err, account.ErrEmailNotVerified):
		return http.StatusUnauthorized
	case errors.Is(err, account.ErrLocked):
		return http.StatusTooManyRequests
	case errors.Is(err, account.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, account.ErrForbidden):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func (s *Server) pageLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.accounts.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, s.cookie(sessionCookie, "", -1, true))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// requireUser redirects signed-out visitors to the login page.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) *account.User {
	u := currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	}
	return u
}

func (s *Server) pageMe(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if r.Method == http.MethodPost {
		nu, err := s.accounts.UpdateSettings(r.Context(), u, app.Settings{DisplayName: r.PostFormValue("name"),
			Dialect: r.PostFormValue("dialect"), Lang: r.PostFormValue("lang"), Level: r.PostFormValue("level")})
		if err != nil {
			m := s.meModel(r)
			m.Error = accountMessage(err)
			renderMe(w, r, statusOf(err), m)
			return
		}
		m := s.meModel(r.WithContext(context.WithValue(r.Context(), userKey, nu)))
		m.Message = "Đã lưu."
		renderMe(w, r, http.StatusOK, m)
		return
	}
	renderMe(w, r, http.StatusOK, s.meModel(r))
}

// meModel is the settings page for the request's user, with the password
// and linked-provider details filled in.
func (s *Server) meModel(r *http.Request) views.MeModel {
	m := views.MeModel{Page: s.page(r)}
	if p, err := s.accounts.Profile(r.Context(), m.Page.User); err == nil {
		m.Page.User = p
	} else {
		s.opts.Log.Error("profile", "err", err)
	}
	return m
}

func renderMe(w http.ResponseWriter, r *http.Request, status int, m views.MeModel) {
	render(w, r, status, views.MePage(m))
}

// providers lists the enabled sign-in providers, for the login page.
func (s *Server) providers() []string {
	out := make([]string, 0, len(s.opts.OAuth))
	for name := range s.opts.OAuth {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// authPageWithError is the login page showing msg (failed provider sign-in).
func (s *Server) authPageWithError(r *http.Request, msg string) templ.Component {
	return views.AuthPage(views.AuthModel{Page: s.page(r), Next: "/", Providers: s.providers(), Error: msg})
}

func (s *Server) pageDeleteMe(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := s.accounts.DeleteAccount(r.Context(), u, r.PostFormValue("password")); err != nil {
		m := s.meModel(r)
		m.Error = accountMessage(err)
		renderMe(w, r, statusOf(err), m)
		return
	}
	http.SetCookie(w, s.cookie(sessionCookie, "", -1, true))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) pageBooks(w http.ResponseWriter, r *http.Request) {
	m := views.BooksModel{Page: s.page(r)}
	u := m.Page.User
	if r.Method == http.MethodPost {
		if s.requireUser(w, r) == nil {
			return
		}
		b, err := s.accounts.CreateBook(r.Context(), u, r.PostFormValue("title"), r.PostFormValue("description"), false)
		if err == nil {
			http.Redirect(w, r, "/books/"+b.ID, http.StatusSeeOther)
			return
		}
		m.Error = accountMessage(err)
	}
	if u == nil {
		m.Guest = guestItems(r)
	} else {
		var err error
		if m.Books, err = s.accounts.Books(r.Context(), u); err != nil {
			m.Error = accountMessage(err)
		}
	}
	render(w, r, http.StatusOK, views.BooksPage(m))
}

func (s *Server) pageBook(w http.ResponseWriter, r *http.Request) {
	u, id := currentUser(r), r.PathValue("id")
	if r.Method == http.MethodPost {
		if s.requireUser(w, r) == nil {
			return
		}
		if _, err := s.accounts.UpdateBook(r.Context(), u, id, r.PostFormValue("title"), r.PostFormValue("description"),
			r.PostFormValue("public") == "true"); err != nil {
			s.renderBook(w, r, id, accountMessage(err), statusOf(err))
			return
		}
		http.Redirect(w, r, "/books/"+id, http.StatusSeeOther)
		return
	}
	s.renderBook(w, r, id, "", http.StatusOK)
}

func (s *Server) renderBook(w http.ResponseWriter, r *http.Request, id, msg string, status int) {
	u := currentUser(r)
	b, items, err := s.accounts.Book(r.Context(), u, id)
	if err != nil {
		render(w, r, statusOf(err), views.BooksPage(views.BooksModel{Page: s.page(r), Error: accountMessage(err)}))
		return
	}
	render(w, r, status, views.BookPage(views.BookModel{Page: s.page(r), Book: b, Items: items,
		Owner: u != nil && u.ID == b.UserID, Error: msg}))
}

func (s *Server) pageDeleteBook(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := s.accounts.DeleteBook(r.Context(), u, r.PathValue("id")); err != nil {
		s.renderBook(w, r, r.PathValue("id"), accountMessage(err), statusOf(err))
		return
	}
	http.Redirect(w, r, "/books", http.StatusSeeOther)
}

func (s *Server) pageAddItem(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id := r.PathValue("id")
	if _, err := s.accounts.AddItem(r.Context(), u, id, app.NewItem{Text: r.PostFormValue("text"), Lang: string(entry.VI)}); err != nil {
		s.renderBook(w, r, id, accountMessage(err), statusOf(err))
		return
	}
	http.Redirect(w, r, "/books/"+id, http.StatusSeeOther)
}

func itemID(r *http.Request) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue("item"), 10, 64)
	if err != nil {
		return 0, account.ErrNotFound
	}
	return n, nil
}

// pageDeleteItem is an HTMX call: an empty 200 removes the row.
func (s *Server) pageDeleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := itemID(r)
	if err == nil {
		err = s.accounts.DeleteItem(r.Context(), currentUser(r), r.PathValue("id"), id)
	}
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) pageItemNote(w http.ResponseWriter, r *http.Request) {
	id, err := itemID(r)
	if err == nil {
		err = s.accounts.UpdateItemNote(r.Context(), currentUser(r), r.PathValue("id"), id, r.PostFormValue("note"))
	}
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pageOrder takes "ids=3,1,2" (from the drag-and-drop script) or a JSON list.
func (s *Server) pageOrder(w http.ResponseWriter, r *http.Request) {
	ids, err := orderIDs(r)
	if err == nil {
		err = s.accounts.Reorder(r.Context(), currentUser(r), r.PathValue("id"), ids)
	}
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func orderIDs(r *http.Request) ([]int64, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&body); err != nil {
			return nil, account.ErrInvalidInput
		}
		return body.IDs, nil
	}
	var ids []int64
	for _, f := range strings.Split(r.FormValue("ids"), ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, account.ErrInvalidInput
		}
		ids = append(ids, n)
	}
	return ids, nil
}

func (s *Server) pageExport(w http.ResponseWriter, r *http.Request) {
	data, ctype, name, err := s.accounts.Export(r.Context(), currentUser(r), r.PathValue("id"), r.URL.Query().Get("format"))
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+url.PathEscape(name))
	_, _ = w.Write(data)
}

// pageSave is the "＋" next to a result: into the chosen (or most recent)
// book for users, into the cookie notebook for guests.
func (s *Server) pageSave(w http.ResponseWriter, r *http.Request) {
	entryID, _ := strconv.ParseInt(r.PostFormValue("entry_id"), 10, 64)
	p := app.DefaultRhymeParams(r.PostFormValue("matched_from"))
	p.Lang = entry.Lang(r.PostFormValue("lang"))
	n := app.NewItem{EntryID: entryID, Lang: r.PostFormValue("lang"), Dialect: r.PostFormValue("dialect"),
		Level: r.PostFormValue("level"), MatchedFrom: r.PostFormValue("matched_from")}
	u := currentUser(r)
	if u == nil {
		items := guestItems(r)
		if len(items) >= account.GuestMaxItems {
			render(w, r, http.StatusOK, views.SaveButton(entryID, p, "Sổ tạm đã đầy (20) — đăng ký để lưu tiếp"))
			return
		}
		e, err := s.svc.EntryByID(entryID)
		if err != nil {
			render(w, r, http.StatusOK, views.SaveButton(entryID, p, "Không tìm thấy mục từ"))
			return
		}
		items = append(items, account.GuestItem{Text: e.Headword, EntryID: entryID, Lang: n.Lang, Dialect: n.Dialect,
			Level: n.Level, MatchedFrom: n.MatchedFrom})
		s.setGuestItems(w, items)
		s.record(r, analytics.Event{Type: analytics.SaveItem, Lang: n.Lang, Dialect: n.Dialect, Level: n.Level,
			EntryID: entryID, Query: queryText(n.MatchedFrom)})
		render(w, r, http.StatusOK, views.SaveButton(entryID, p, "guest"))
		return
	}
	bookID := r.PostFormValue("book_id")
	if bookID == "" {
		books, err := s.accounts.Books(r.Context(), u)
		if err != nil {
			render(w, r, http.StatusOK, views.SaveButton(entryID, p, accountMessage(err)))
			return
		}
		if len(books) > 0 {
			bookID = books[0].ID
		} else {
			b, err := s.accounts.CreateBook(r.Context(), u, "Sổ của tôi", "", false)
			if err != nil {
				render(w, r, http.StatusOK, views.SaveButton(entryID, p, accountMessage(err)))
				return
			}
			bookID = b.ID
		}
	}
	if _, err := s.accounts.AddItem(r.Context(), u, bookID, n); err != nil {
		render(w, r, http.StatusOK, views.SaveButton(entryID, p, accountMessage(err)))
		return
	}
	s.record(r, analytics.Event{Type: analytics.SaveItem, Lang: n.Lang, Dialect: n.Dialect, Level: n.Level,
		EntryID: entryID, Query: queryText(n.MatchedFrom)})
	render(w, r, http.StatusOK, views.SaveButton(entryID, p, "saved"))
}

func writeAccountProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := statusOf(err)
	if status == http.StatusInternalServerError {
		writeProblem(w, r, err)
		return
	}
	slug := strings.ReplaceAll(strings.ToLower(http.StatusText(status)), " ", "-")
	writeProblemStatus(w, r, status, slug, accountMessage(err), err.Error())
}
