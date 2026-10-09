package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"playword/internal/app"
	"playword/internal/domain/rhyme"
	"playword/pkg/vnphon"
)

// problem is an RFC 7807 error body.
type problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// classify maps domain errors to an HTTP status and a stable problem type.
func classify(err error) (int, string, string) {
	switch {
	case errors.Is(err, vnphon.ErrInvalidSyllable):
		return http.StatusUnprocessableEntity, "invalid-syllable", "Không tách được âm tiết tiếng Việt"
	case errors.Is(err, app.ErrNoPronunciation):
		return http.StatusUnprocessableEntity, "no-pronunciation", "Không có phát âm cho từ này"
	case errors.Is(err, rhyme.ErrInvalidQuery), errors.Is(err, app.ErrBadCursor), errors.Is(err, app.ErrInvalidParam):
		return http.StatusBadRequest, "invalid-query", "Tham số không hợp lệ"
	case errors.Is(err, app.ErrUnsupportedLang):
		return http.StatusBadRequest, "unsupported-language", "Ngôn ngữ chưa được hỗ trợ"
	case errors.Is(err, app.ErrNotFound):
		return http.StatusNotFound, "not-found", "Không tìm thấy"
	case errors.Is(err, app.ErrNotReady):
		return http.StatusServiceUnavailable, "not-ready", "Chỉ mục đang được nạp"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout", "Quá thời gian xử lý"
	}
	return http.StatusInternalServerError, "internal", "Lỗi máy chủ"
}

func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, slug, title := classify(err)
	detail := err.Error()
	if status == http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "err", err)
		detail = "" // do not leak internals
	}
	writeProblemStatus(w, r, status, slug, title, detail)
}

func writeProblemStatus(w http.ResponseWriter, r *http.Request, status int, slug, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Type: "https://playword.app/problems/" + slug, Title: title,
		Status: status, Detail: detail, Instance: r.URL.Path})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
