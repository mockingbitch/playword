---
name: ship
description: Final gate before committing Playword work — generated code, format, lint, race tests, integration tests, UI check, docs and privacy page, then a clear commit on a feature branch. Use when the user says the work is done, asks to commit, or asks to prepare a PR.
disable-model-invocation: true
argument-hint: "[commit message summary]"
---
# Ship

1. `git status` and `git diff --stat`: everything intended, nothing stray (scratch files, `.uicheck-out/`,
   binaries, `.env`). On `master`/`main`? Create a branch first.
2. `make check` must pass. If SQL/migrations changed: `make test-integration`. If UI changed: `make uicheck`
   against a running build.
3. Docs in the same change: README (features, env vars), `.env.example`, runbook (operations), `/privacy`
   (data handling), `.docs/plans/*` status, ADR if a decision was made (`/adr`).
4. Scan the staged diff for secrets (`git diff --cached | grep -nE 'GOCSPX|secret|password=|BEGIN .*PRIVATE'`).
5. Commit with a message: imperative subject ≤ 72 chars (`feat:`, `fix:`, `perf:`, `refactor:`, `docs:`,
   `test:`), a body grouped by area explaining what and why, migrations and deploy steps called out
   (`rebuild-keys`, new env vars). $ARGUMENTS
6. Do not push, merge or tag unless the user asks. Report the commit hash, branch and what the deploy needs.
