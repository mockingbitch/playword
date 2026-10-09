package httpadapter

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const adminToken = "test-admin-token-0123456789abcdef0123"

func TestSlangSubmitRateLimit(t *testing.T) {
	ts, _, _ := testServerWith(t, Options{SubmitRPM: 1})
	b := newBrowser(t, ts)
	if r, _ := b.form("/slang/new", url.Values{"headword": {"chill"}, "gloss": {"thư giãn"}}); r.StatusCode != 200 {
		t.Fatalf("first: %d", r.StatusCode)
	}
	if r, _ := b.json("POST", "/api/v1/slang", map[string]string{"headword": "flex", "gloss": "khoe"}); r.StatusCode != 429 {
		t.Errorf("second submission within a minute: %d (spec: 1 per minute)", r.StatusCode)
	}
}

func TestSlangSubmitAndReview(t *testing.T) {
	ts, _, users := testServerWith(t, Options{AdminToken: adminToken})
	guest := newBrowser(t, ts)

	r, body := guest.form("/slang/new", url.Values{"headword": {"chill"}, "lang": {"vi"}, "gloss": {"thư giãn"}, "respelling": {"chiu"}})
	if r.StatusCode != 200 || !strings.Contains(body, "đang chờ duyệt") {
		t.Fatalf("submit: %d", r.StatusCode)
	}
	if r, body := guest.form("/slang/new", url.Values{"headword": {""}, "gloss": {"khoe"}}); r.StatusCode != 422 || !strings.Contains(body, "Chưa hợp lệ") {
		t.Errorf("invalid submission: %d", r.StatusCode)
	}

	// Admin routes hide from everyone else.
	if r, _ := guest.do("GET", "/api/v1/admin/slang", "", nil); r.StatusCode != 404 {
		t.Errorf("anonymous admin API: %d", r.StatusCode)
	}
	if r, _ := guest.do("GET", "/admin/slang", "", nil); r.StatusCode != 303 {
		t.Errorf("anonymous admin page should ask to log in: %d", r.StatusCode)
	}
	user := newBrowser(t, ts)
	user.form("/register", url.Values{"email": {"u@example.vn"}, "password": {"correct horse battery"}})
	if r, _ := user.do("GET", "/admin/slang", "", nil); r.StatusCode != 404 {
		t.Errorf("non-admin: %d", r.StatusCode)
	}

	// The bearer token works without cookies or CSRF.
	r, body = guest.do("GET", "/api/v1/admin/slang", "", nil, "Authorization", "Bearer "+adminToken)
	if r.StatusCode != 200 || !strings.Contains(body, `"respelling":"chiu"`) || !strings.Contains(body, `"submitted_by":"guest:`) {
		t.Fatalf("list: %d %s", r.StatusCode, body)
	}
	if r, _ := guest.do("GET", "/api/v1/admin/slang", "", nil, "Authorization", "Bearer wrong"); r.StatusCode != 404 {
		t.Errorf("wrong token: %d", r.StatusCode)
	}
	r, body = guest.do("POST", "/api/v1/admin/slang/1/approve", "", nil, "Authorization", "Bearer "+adminToken)
	if r.StatusCode != 200 || !strings.Contains(body, `"status":"approved"`) || !strings.Contains(body, `"entry_id"`) {
		t.Fatalf("approve: %d %s", r.StatusCode, body)
	}
	if r, _ := guest.do("POST", "/api/v1/admin/slang/1/approve", "", nil, "Authorization", "Bearer "+adminToken); r.StatusCode != 422 {
		t.Errorf("approve twice: %d", r.StatusCode)
	}
	if r, body := guest.do("POST", "/api/v1/admin/entries/1001/trend", "application/json", strings.NewReader(`{"delta":0.5}`), "Authorization", "Bearer "+adminToken); r.StatusCode != 200 || !strings.Contains(body, `"trend_score":1.5`) {
		t.Errorf("bump: %d %s", r.StatusCode, body)
	}

	// An admin by role reviews from the HTML page with HTMX.
	users.SetRole("u@example.vn", "admin")
	user2 := newBrowser(t, ts)
	user2.form("/login", url.Values{"email": {"u@example.vn"}, "password": {"correct horse battery"}})
	if r, body := user2.json("POST", "/api/v1/slang", map[string]string{"headword": "ét o ét", "gloss": "cứu với"}); r.StatusCode != 201 {
		t.Fatalf("api submit: %d %s", r.StatusCode, body)
	}
	if _, body := user2.do("GET", "/admin/slang", "", nil); !strings.Contains(body, "ét o ét") || !strings.Contains(body, `hx-post="/admin/slang/2/reject"`) {
		t.Errorf("admin page: %s", body)
	}
	if r, body := user2.do("POST", "/admin/slang/2/reject", "", nil, "X-CSRF-Token", user2.csrf, "HX-Request", "true"); r.StatusCode != 200 || !strings.Contains(body, "rejected") {
		t.Errorf("reject row: %d %s", r.StatusCode, body)
	}
	if r, _ := user2.do("POST", "/api/v1/admin/ingest/hnd", "", nil, "X-CSRF-Token", user2.csrf); r.StatusCode != 404 {
		t.Errorf("ingest without an ingester configured: %d", r.StatusCode)
	}
	if r, _ := user2.do("GET", "/admin", "", nil); r.StatusCode != http.StatusSeeOther {
		t.Errorf("/admin: %d", r.StatusCode)
	}
}
