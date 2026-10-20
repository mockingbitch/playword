---
name: migration
description: Add a database change to Playword safely — goose migration, sqlc queries, store methods, the in-memory fake and integration tests. Use when a feature needs a new table, column, index or query.
argument-hint: "<short_name> <what changes>"
---
# Database change: $ARGUMENTS

1. Next number: `ls migrations/` → `migrations/000NN_<short_name>.sql`. Write `-- +goose Up` and a real
   `-- +goose Down`. Defaults for new NOT NULL columns so existing rows stay valid. Big tables: create
   indexes `concurrently` in their own migration (`-- +goose NO TRANSACTION`).
2. Queries in `internal/adapters/postgres/<area>.sql` (`-- name: … :one|:many|:exec|:execrows`), then
   `make sqlc`. Never edit `internal/adapters/postgres/db/`.
3. Domain type fields → port method (`internal/ports`) → postgres store method (map no-rows to
   `ErrNotFound`, ownership in the `where`) → fake in `internal/app/apptest` with the same semantics.
4. Integration test with `testStore(t)` (fresh database per test).
5. Apply locally: `make migrate`, then `make test-integration` and `make check`.
6. If the change affects derived data (`rhyme_keys`), say in the report that `make rebuild-keys` is needed on
   deploy; mention the migration in the commit message.
