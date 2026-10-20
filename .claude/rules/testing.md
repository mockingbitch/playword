---
paths:
  - "**/*_test.go"
  - "**/testdata/**"
---
# Tests

- Table-driven tests with `t.Run` names when cases share a body; plain sequential assertions when a story
  reads better (the HTTP tests do this). Fail messages say what was expected: `t.Errorf("x = %v, want %v")`.
- Domain and `pkg/` code: unit tests + **fuzz** for anything parsing text (`FuzzParseSyllable`, `FuzzCheck`),
  with invariants checked inside the fuzz body (offsets in range, no one-syllable fragments …).
- App layer: use the in-memory fakes in `internal/app/apptest`; extend them when a port grows.
- HTTP: `testServerWith(t, Options{…})` and the `browser` helper (cookie jar + CSRF). Cover the no-JavaScript
  path (plain form post → 303) as well as the HTMX path (`HX-Request: true`).
- Postgres: integration tests skip unless `PLAYWORD_TEST_DATABASE_URL` is set; each test gets a fresh
  database from `testStore(t)`. Run `make test-integration` when SQL or migrations change.
- Golden files (`testdata/*.golden.html`): regenerate with `-update` **only after reading the diff**.
- Benchmarks next to the code, `b.Loop()`, realistic data.
- Always `-race`. No sleeps for synchronisation — use channels, `synctest`, or polling with a deadline.
- Never delete or loosen a test to get green. If behaviour changed on purpose, change the assertion and
  mention it in the report.
