# Architecture (hexagonal) — where code goes

- `pkg/vnphon`, `internal/domain/**`: **pure**. No database, network, files, clock, randomness, logging or
  globals. Deterministic functions over values; everything else is passed in. This is what keeps the engine
  fast, fuzzable and explainable.
- `internal/ports`: interfaces the app depends on (stores, sources, OAuth). Change a port → update the
  postgres adapter **and** the in-memory fake in `internal/app/apptest` in the same change.
- `internal/app`: use cases. Validates input, enforces rules (ownership, limits), calls ports and the domain.
  Returns domain/sentinel errors. No HTTP types.
- `internal/adapters/*`: the outside world. `http` (handlers, views, static), `postgres` (sqlc), `index`
  (in-memory snapshot rebuilt on NOTIFY), `sources` (ingest readers), `oauth`, `observability`.
- `cmd/*`: wiring and env config only.

Rules of thumb: a handler parses → calls one app method → renders. Business rules never live in templates
or handlers. A new feature usually touches every layer once; if a change needs a domain import from an
adapter type, the boundary is wrong.
