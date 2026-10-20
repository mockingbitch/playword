---
name: feature
description: Implement a Playword feature end to end through the hexagonal layers (domain → ports → postgres + fake → app → http → views → tests → docs) with the project's quality gates. Use when the user asks to build or extend a feature.
argument-hint: "<feature description>"
---
# Implement a feature: $ARGUMENTS

Work in this order; skip layers the feature does not touch, but never skip verification.

0. **Understand and decide.** Read CLAUDE.md and the rules for the paths you will touch. Find the closest
   existing feature and copy its shape. List the decisions the feature needs. Anything about rhyme rules,
   privacy, pricing or product behaviour that is not already decided in .docs/ → ask the user with a
   recommended option (AskUserQuestion) before writing code. Make sure you are on a feature branch.
1. **Domain** (`internal/domain`, `pkg/`): pure types and functions, table tests and, for text input, a fuzz
   target with invariants. Benchmark if it is on a hot path.
2. **Ports** (`internal/ports`): the smallest interface change. Update `internal/app/apptest` fakes.
3. **Postgres**: migration (`/migration`), queries in `*.sql`, `make sqlc`, store methods, integration test.
4. **App** (`internal/app`): validation, limits, ownership, sentinel errors; tests with fakes.
5. **HTTP**: handler (parse → one app call → render), route with the right wrapper (`search`/`login`/
   `plain`/`admin`), CSRF, bounded input, JSON API + HTML page, no-JS fallback. Tests for both paths.
6. **Views** (`/ui-change`): templ + tokens in app.css, accessible, responsive, Vietnamese copy.
7. **Config/docs**: env var in `loadConfig` + README table + `.env.example`; README feature list; runbook if
   operations change; `/privacy` if data handling changes; a plan in `.docs/plans/` for anything sizeable.
8. **Verify**: `make check`; `make test-integration` if SQL changed; run the app and `make uicheck` for UI;
   ask the `go-reviewer` agent to review the diff and fix what it finds.
9. **Report** in Vietnamese: what changed per layer, how it was verified (commands + results), what is not
   done, open questions. Commit only if the user asks.
