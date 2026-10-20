---
paths:
  - "internal/adapters/http/*.go"
  - "cmd/playword/**"
---
# HTTP adapter

- One handler = parse params → one app call → render. Errors: `writeProblem` / `writeAccountProblem`
  (RFC 7807 `application/problem+json`) for the API; Vietnamese message pages for HTML via `accountMessage`,
  `userMessage`, `statusOf`.
- Register routes in `server.go` with the right wrapper: `search(…)` (rate limit + 2 s timeout) for search-like
  work, `login(…)` for auth-sensitive posts (5/min/IP), `plain(…)` otherwise, `s.admin(…)` for admin.
- Every unsafe method passes the double-submit CSRF check (`identify`); forms carry `@csrfField`, HTMX sends
  the header from `<body hx-headers>`, `fetch` sends `X-CSRF-Token`.
- Bound every input: body size (`decodeJSON` limits), text length (`MaxLyrics`, `MaxCheckText`), page size.
- Redirect targets go through `safeNext`. Cookies through `s.cookie(...)` (Secure unless
  `PLAYWORD_INSECURE_COOKIES`, HttpOnly where JS does not need them, SameSite=Lax).
- Analytics: `s.record(r, analytics.Event{…})` — never the user's lyrics or free text beyond the normalised
  query; throttle anything a client could send in a loop.
- New config comes in as an env var in `cmd/playword/main.go` `loadConfig` (validated, documented in the
  README table and `.env.example`).
