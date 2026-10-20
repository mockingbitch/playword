---
paths:
  - "migrations/**"
  - "internal/adapters/postgres/**"
  - "sqlc.yaml"
---
# Database — goose + sqlc + pgx

- **Migrations are forward-only in practice**: never edit a merged migration; add `000NN_name.sql` with
  `-- +goose Up` and a working `-- +goose Down`. Use `-- +goose StatementBegin/End` around functions. Keep them
  idempotent-safe for re-runs (`if not exists` where it helps).
- Queries live in `internal/adapters/postgres/*.sql` with `-- name: X :one|:many|:exec|:execrows`; regenerate
  with `make sqlc`. **Never edit `internal/adapters/postgres/db/`.** Use `sqlc.arg()`/`@name` and do date
  arithmetic in Go when sqlc mangles repeated params.
- Store methods translate `pgx.ErrNoRows` → `ErrNotFound`, unique violations → domain errors, `22P02` (bad
  UUID) → `ErrNotFound`. Ownership checks go in the `where` (`id = $1 and user_id = $2`) and `:execrows`.
- Bulk loads: COPY into a temp stage table, then set-based SQL (see `internal/ingest`).
- `rhyme_keys` is derived: after `KeyerVersion` or `configs/*.yaml` change, `make rebuild-keys`.
- Every port change: postgres store + `apptest` fake + an integration test (`testStore(t)`).
