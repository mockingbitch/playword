---
paths:
  - "**/*.go"
---
# Go style (Go 1.27, this repository)

Follow Effective Go, the Go Code Review Comments and what the surrounding code already does. Specifics:

**Language and standard library — use the modern forms**
- `min`/`max`/`clear` builtins; `slices`, `maps`, `cmp` (`cmp.Compare`, `cmp.Or`) instead of hand-written loops
  and sort.Slice; `strings.Cut`/`CutPrefix`; `for i := range n`; range-over-func iterators (`iter.Seq`) when a
  callback API would otherwise allocate a slice.
- `log/slog` only (structured key/value, no `fmt.Printf` logging); `errors.Join`; `context.WithoutCancel` /
  `AfterFunc`; `sync.OnceValue(s)`; `sync.WaitGroup.Go`; `atomic.Pointer[T]` for read-mostly snapshots (see
  `rhymeindex` RCU).
- Tests: `t.Context()`, `b.Loop()` for benchmarks, `testing/synctest` for time-dependent code, `t.Setenv`.
- Generics when they remove real duplication (containers, small helpers); not for one call site.

**Errors**
- Wrap with `fmt.Errorf("doing x: %w", err)`; match with `errors.Is`/`errors.As`. Sentinel errors live with
  their domain (`account.ErrNotFound`, `rhyme.ErrInvalidQuery`, `app.ErrInvalidParam`) and are mapped to HTTP
  status in one place (`problem.go`, `statusOf`).
- No panics for expected failures. Return early; keep the happy path unindented.
- Error strings are lower case, no trailing punctuation (staticcheck ST1005), in English: details after a
  sentinel name the field (`fmt.Errorf("%w: display name", account.ErrInvalidInput)`). Vietnamese wording
  belongs to the HTTP layer (`accountMessage`, `userMessage`); a new error that needs its own message gets a
  case there. (One legacy exception: the lyrics-length detail in `app.SetLyrics` is Vietnamese.)

**APIs and packages**
- `ctx context.Context` first; never store a context in a struct.
- Accept interfaces (small, in `internal/ports`, defined by the consumer), return concrete types.
- Constructors `New…`; zero values useful where cheap. No package-level mutable state; configuration flows
  from `cmd/playword` via options structs.
- Every package has a doc comment; exported identifiers have comments that say what and why, not how.
  Comment density and tone: match the file.
- Names: short in small scopes, descriptive at package level; no stutter (`rhyme.Engine`, not
  `rhyme.RhymeEngine`); acronyms upper case (`URL`, `ID`, `HTTP`).

**Concurrency**
- Every goroutine has an owner and an exit (context or closed channel); no fire-and-forget except the bounded
  `Recorder` pattern. Run `go test -race`.
- Protect shared state with the narrowest primitive; prefer immutable snapshots swapped atomically.

**Formatting and tooling** — gofmt (a hook runs it), `go vet`, golangci-lint (`.golangci.yml`: errcheck, govet,
staticcheck, gocritic, revive, unused, ineffassign). No `//nolint` without a reason on the same line.
