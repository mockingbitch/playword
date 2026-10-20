# Playword

Rhyme dictionary for Vietnamese rappers (VI + EN): Go 1.27, templ + HTMX in one binary, PostgreSQL 17,
in-memory rhyme index. The owner is a senior Go developer and product owner; **talk to him in Vietnamese**,
keep code, comments, identifiers and commit messages in English.

## Non-negotiables (product)

1. **Never generate words.** Every result is a real entry from an ingested source; nothing is synthesised,
   guessed or "AI-completed". Site examples are live search results.
2. **Every result is explainable**: it has at least one sense, and its rhyme level is shown.
3. **The rhyme engine is pure**: `pkg/vnphon` and `internal/domain/**` do no I/O, no logging, no globals,
   no time.Now. Inputs in, values out.
4. **When a rhyme rule is unclear, ask; don't guess.** Phonology, dialect mappings, level semantics and
   grouping rules are product decisions (see `.docs/adr/`, `.docs/plans/`). Write the question, offer a
   recommended option, stop.

## Map

```
cmd/playword          server (12-factor env config, binds ports first, graceful shutdown)
cmd/playword-cli      migrate, ingest, rebuild-keys, freq, user-role, user-delete, maintain, dialect-check …
pkg/vnphon            Vietnamese syllable parser/renderer (pure, fuzzed)
internal/domain/      rhyme (keyer, levels, dialects, engine, check), entry, account, slang, analytics — pure
internal/ports/       interfaces the app needs (stores, sources, OAuth provider)
internal/app/         use cases: Service (search), Accounts, Slang, Ingester, Dashboard, Recorder
internal/adapters/    http (handlers + views/*.templ + static/), postgres (sqlc in db/), index (CSR, lookup,
                      rhymeindex snapshot), sources (hnd, oewn, cmudict, wiktionary…), oauth, observability
internal/ingest/      staged ingest pipeline + scheduler
migrations/           goose, forward-only, embedded
configs/              rhyme_families.yaml, dialects.yaml — their hash is part of ConfigID (KeyerVersion)
.docs/                 adr/, plans/, runbook.md, ui-review/
.scripts/              benchgate (CI perf gate), uicheck (headless Chrome UI checks, own module), k6, backup
```

Dependency direction: `adapters → app → ports → domain`; domain imports nothing of ours except `pkg/`.

## Commands

| Need | Command |
|---|---|
| Definition of done | `make check` — templ generate, gofmt, vet, golangci-lint (or staticcheck), `go test -race` |
| Run locally | `make db-up && make run` → http://localhost:8020 (dev DB on 127.0.0.1:55432) |
| Integration tests | `make test-integration` (needs the dev DB) |
| After editing `.templ` | `make generate` (a hook also does it per file) |
| After editing SQL queries | `make sqlc` (docker) — never edit `internal/adapters/postgres/db/` |
| Migrations | `make migrate`; new file `migrations/000NN_name.sql` with `-- +goose Up/Down` |
| Rhyme config or keyer changed | bump `KeyerVersion`, then `make rebuild-keys` |
| Benchmarks | `go test ./<pkg> -run '^$' -bench . -benchmem`; CI fails at >30% slower (.scripts/benchgate) |
| UI check | server running, then `make uicheck` (overflow, console/CSP errors, screenshots in `.uicheck-out/`) |
| Fuzz | `make fuzz` (vnphon), `go test ./internal/domain/rhyme -run '^$' -fuzz FuzzCheck -fuzztime 30s` |

## How to work here

- **Read before you write.** Find the existing pattern (a sibling handler, store method, test helper) and copy
  its shape. The code base is consistent; keep it so.
- **Small vertical slices** through the layers: domain → ports → postgres/fake → app → http → views → tests →
  docs. The project skills `/feature`, `/migration`, `/ui-change`, `/perf`, `/ship` walk these.
- **Tests change with behaviour.** Never delete or weaken a test to make it pass; if markup or copy changed on
  purpose, update the assertion and say so. Golden files: rerun with `-update` only after reading the diff.
- **Measure, don't guess** performance: benchmark before and after, report the numbers.
- **Verify UI in a browser**, not only by tests: `make uicheck`, then look at the screenshots.
- **Generated files** (`*_templ.go`, `postgres/db/*.go`) are outputs; a hook blocks editing them.
- **No new dependency** without a stated reason (what it replaces, size, licence). Prefer the standard library.
- **Git:** work on a branch, commit only when asked, never push or rewrite history without asking.
- **Report** in Vietnamese, short: what changed, how it was verified, what is left, open questions.

## Open product questions (do not decide them silently)

Q12 `analyst` role · Q13 cross VI–EN rhymes · Q14 identity rhymes ("old money") · Nghệ–Tĩnh dialect ·
legal review of /terms and /privacy before launch.
