package httpadapter

import (
	"net/http"
	"net/url"

	"playword/internal/adapters/http/views"
	"playword/internal/domain/account"
)

// pageWrite opens the writing desk: a guest's desk keeps the draft in the
// browser; a user goes to their most recent book, or starts one.
func (s *Server) pageWrite(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil || s.accounts == nil {
		s.renderDesk(w, r, nil, nil, "")
		return
	}
	books, err := s.accounts.Books(r.Context(), u)
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	if len(books) == 0 {
		render(w, r, http.StatusOK, views.DeskStart(s.page(r)))
		return
	}
	http.Redirect(w, r, "/books/"+books[0].ID+"/write", http.StatusSeeOther)
}

// pageBookWrite is the desk for one of the user's books.
func (s *Server) pageBookWrite(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	b, _, err := s.accounts.Book(r.Context(), u, r.PathValue("id"))
	if err == nil && b.UserID != u.ID {
		err = account.ErrNotFound // a public book can be read, not written
	}
	if err != nil {
		s.renderBook(w, r, r.PathValue("id"), accountMessage(err), statusOf(err))
		return
	}
	books, _ := s.accounts.Books(r.Context(), u)
	s.renderDesk(w, r, b, books, "")
}

func (s *Server) renderDesk(w http.ResponseWriter, r *http.Request, b *account.Book, books []account.Book, msg string) {
	m := views.DeskModel{Page: s.page(r), Book: b, Books: books, Error: msg, Saved: r.URL.Query().Get("saved") != ""}
	m.Params, _ = rhymeParamsFor(url.Values{}, m.Page.User)
	status := http.StatusOK
	if msg != "" {
		status = http.StatusBadRequest
	}
	render(w, r, status, views.DeskPage(m))
}

// pageLyrics saves a book's lyrics: autosaved by HTMX, or posted by the
// desk's "Lưu" button without JavaScript.
func (s *Server) pageLyrics(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id := r.PathValue("id")
	err := s.accounts.SetLyrics(r.Context(), u, id, r.PostFormValue("lyrics"))
	if isHTMX(r) {
		if err != nil {
			render(w, r, http.StatusOK, views.LyricsSaved(accountMessage(err), false))
			return
		}
		render(w, r, http.StatusOK, views.LyricsSaved("Đã lưu.", true))
		return
	}
	if err != nil {
		b, _, berr := s.accounts.Book(r.Context(), u, id)
		if berr != nil {
			s.renderBook(w, r, id, accountMessage(berr), statusOf(berr))
			return
		}
		b.Lyrics = r.PostFormValue("lyrics") // keep what was typed
		books, _ := s.accounts.Books(r.Context(), u)
		s.renderDesk(w, r, b, books, accountMessage(err))
		return
	}
	http.Redirect(w, r, "/books/"+id+"/write?saved=1", http.StatusSeeOther)
}
