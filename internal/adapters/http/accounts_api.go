package httpadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"playword/internal/app"
	"playword/internal/domain/account"
	"playword/internal/domain/analytics"
)

// JSON API for accounts and books (spec addendum §6). Every unsafe method
// needs the X-CSRF-Token header; GET /api/v1/auth/csrf hands it out.

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return account.ErrInvalidInput
	}
	return nil
}

type userJSON struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name"`
	DefaultDialect string    `json:"default_dialect"`
	DefaultLang    string    `json:"default_lang"`
	DefaultLevel   string    `json:"default_level"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

func toUserJSON(u *account.User) userJSON {
	return userJSON{u.ID, u.Email, u.DisplayName, u.DefaultDialect, u.DefaultLang, u.DefaultLevel, u.Role, u.CreatedAt}
}

type bookJSON struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	IsPublic    bool       `json:"is_public"`
	ItemCount   int        `json:"item_count"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Items       []itemJSON `json:"items,omitempty"`
}

type itemJSON struct {
	ID          int64  `json:"id"`
	EntryID     int64  `json:"entry_id,omitempty"`
	Text        string `json:"text"`
	Note        string `json:"note,omitempty"`
	Lang        string `json:"lang"`
	Dialect     string `json:"dialect,omitempty"`
	Level       string `json:"level,omitempty"`
	MatchedFrom string `json:"matched_from,omitempty"`
	Ord         int    `json:"ord"`
}

func toBookJSON(b *account.Book, items []account.Item) bookJSON {
	out := bookJSON{ID: b.ID, Title: b.Title, Description: b.Description, IsPublic: b.IsPublic, ItemCount: b.ItemCount, UpdatedAt: b.UpdatedAt}
	for _, it := range items {
		out.Items = append(out.Items, toItemJSON(it))
	}
	if items != nil && out.Items == nil {
		out.Items = []itemJSON{}
	}
	return out
}

func toItemJSON(it account.Item) itemJSON {
	return itemJSON{it.ID, it.EntryID, it.Text, it.Note, it.Lang, it.Dialect, it.Level, it.MatchedFrom, it.Ord}
}

func (s *Server) apiCSRF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": csrfToken(r), "header": csrfHeader})
}

func (s *Server) apiAuth(register bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email       string `json:"email"`
			Password    string `json:"password"`
			DisplayName string `json:"display_name"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		ip, ua := clientIP(r, s.opts.TrustProxy), r.UserAgent()
		var u *account.User
		var token string
		var err error
		if register {
			u, token, err = s.accounts.Register(r.Context(), body.Email, body.Password, body.DisplayName, ip, ua)
		} else {
			u, token, err = s.accounts.Login(r.Context(), body.Email, body.Password, ip, ua)
		}
		if err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		s.setSession(w, token)
		s.record(r.WithContext(context.WithValue(r.Context(), userKey, u)), analytics.Event{Type: analytics.Login})
		status := http.StatusOK
		if register {
			status = http.StatusCreated
		}
		writeJSON(w, status, toUserJSON(u))
	}
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.accounts.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, s.cookie(sessionCookie, "", -1, true))
	w.WriteHeader(http.StatusNoContent)
}

// apiUser returns the signed-in user or writes a 401.
func apiUser(w http.ResponseWriter, r *http.Request) *account.User {
	u := currentUser(r)
	if u == nil {
		writeAccountProblem(w, r, account.ErrUnauthenticated)
	}
	return u
}

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	u := apiUser(w, r)
	if u == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, toUserJSON(u))
	case http.MethodPatch:
		var body struct {
			DisplayName    *string `json:"display_name"`
			DefaultDialect *string `json:"default_dialect"`
			DefaultLang    *string `json:"default_lang"`
			DefaultLevel   *string `json:"default_level"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		st := app.Settings{DisplayName: u.DisplayName, Dialect: u.DefaultDialect, Lang: u.DefaultLang, Level: u.DefaultLevel}
		for _, f := range []struct {
			src *string
			dst *string
		}{{body.DisplayName, &st.DisplayName}, {body.DefaultDialect, &st.Dialect}, {body.DefaultLang, &st.Lang}, {body.DefaultLevel, &st.Level}} {
			if f.src != nil {
				*f.dst = *f.src
			}
		}
		nu, err := s.accounts.UpdateSettings(r.Context(), u, st)
		if err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toUserJSON(nu))
	case http.MethodDelete:
		var body struct {
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		if err := s.accounts.DeleteAccount(r.Context(), u, body.Password); err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		http.SetCookie(w, s.cookie(sessionCookie, "", -1, true))
		w.WriteHeader(http.StatusNoContent)
	}
}

type bookBody struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	IsPublic    *bool   `json:"is_public"`
}

func (s *Server) apiBooks(w http.ResponseWriter, r *http.Request) {
	u := apiUser(w, r)
	if u == nil {
		return
	}
	if r.Method == http.MethodPost {
		var body bookBody
		if err := decodeJSON(r, &body); err != nil || body.Title == nil {
			writeAccountProblem(w, r, account.ErrInvalidInput)
			return
		}
		desc := ""
		if body.Description != nil {
			desc = *body.Description
		}
		b, err := s.accounts.CreateBook(r.Context(), u, *body.Title, desc, body.IsPublic != nil && *body.IsPublic)
		if err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toBookJSON(b, nil))
		return
	}
	books, err := s.accounts.Books(r.Context(), u)
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	out := make([]bookJSON, len(books))
	for i := range books {
		out[i] = toBookJSON(&books[i], nil)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiBook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		b, items, err := s.accounts.Book(r.Context(), currentUser(r), id)
		if err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toBookJSON(b, items))
	case http.MethodPatch:
		u := apiUser(w, r)
		if u == nil {
			return
		}
		b, _, err := s.accounts.Book(r.Context(), u, id)
		if err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		var body bookBody
		if err := decodeJSON(r, &body); err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		title, desc, public := b.Title, b.Description, b.IsPublic
		if body.Title != nil {
			title = *body.Title
		}
		if body.Description != nil {
			desc = *body.Description
		}
		if body.IsPublic != nil {
			public = *body.IsPublic
		}
		nb, err := s.accounts.UpdateBook(r.Context(), u, id, title, desc, public)
		if err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toBookJSON(nb, nil))
	case http.MethodDelete:
		if err := s.accounts.DeleteBook(r.Context(), currentUser(r), id); err != nil {
			writeAccountProblem(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) apiAddItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryID     int64  `json:"entry_id"`
		CustomText  string `json:"custom_text"`
		Note        string `json:"note"`
		Lang        string `json:"lang"`
		Dialect     string `json:"dialect"`
		Level       string `json:"level"`
		MatchedFrom string `json:"matched_from"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	it, err := s.accounts.AddItem(r.Context(), currentUser(r), r.PathValue("id"), app.NewItem{EntryID: body.EntryID,
		Text: body.CustomText, Note: body.Note, Lang: body.Lang, Dialect: body.Dialect, Level: body.Level, MatchedFrom: body.MatchedFrom})
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	s.record(r, analytics.Event{Type: analytics.SaveItem, Lang: body.Lang, Dialect: body.Dialect, Level: body.Level,
		EntryID: body.EntryID, Query: queryText(body.MatchedFrom)})
	writeJSON(w, http.StatusCreated, toItemJSON(*it))
}

func (s *Server) apiItem(w http.ResponseWriter, r *http.Request) {
	id, err := itemID(r)
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	if r.Method == http.MethodDelete {
		err = s.accounts.DeleteItem(r.Context(), currentUser(r), r.PathValue("id"), id)
	} else {
		var body struct {
			Note string `json:"note"`
		}
		if err = decodeJSON(r, &body); err == nil {
			err = s.accounts.UpdateItemNote(r.Context(), currentUser(r), r.PathValue("id"), id, body.Note)
		}
	}
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiPublicBook(w http.ResponseWriter, r *http.Request) {
	b, items, err := s.accounts.Book(r.Context(), nil, r.PathValue("id"))
	if err != nil {
		writeAccountProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, toBookJSON(b, items))
}
