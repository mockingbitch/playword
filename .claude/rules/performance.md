---
paths:
  - "pkg/**/*.go"
  - "internal/domain/**/*.go"
  - "internal/adapters/index/**/*.go"
  - "internal/app/service.go"
  - "internal/ingest/**/*.go"
  - ".scripts/benchgate/**"
---
# Performance — hot paths and budgets

Budgets the project already meets; a change must not regress them:

| Path | Budget | Now |
|---|---|---|
| `GET /api/v1/rhymes` p99 at 500 rps | < 50 ms | ~6 ms |
| rhyme check, 16-line verse | < 2 ms | 0.7 ms |
| rhyme check, 20k chars worst case | < 20 ms | 14 ms |
| index build (170k entries) | < 15 s, memory ≈ 2× snapshot | ~8 s |
| analytics recording overhead per request | < 0.5 ms, never blocks | 0.14 ms |
| hand-written CSS + JS | < 30 KB gzip | ~18 KB |

How to work:
1. **Benchmark first**: `go test ./<pkg> -run '^$' -bench <Name> -benchmem -count 6`, save the output.
   Write the benchmark with `b.Loop()` and realistic input (real lyrics, real queries from `.scripts/k6`).
2. **Profile, don't guess**: `-cpuprofile` / `-memprofile`, then `go tool pprof -top` and `-list <func>`.
3. Change one thing; rerun; report before → after (ns/op, B/op, allocs/op). CI's benchgate fails at >30%.

Patterns that work here:
- Pack data into integers (`rhyme.Code` uint16) and compare integers in inner loops, not strings.
- Precompute per distinct input (word caches in `Check`), reuse buffers across iterations (`runBuf`), flat
  slices (`i*w+j`) over `[][]T`, `clear(buf)` over re-allocating.
- CSR / sorted arrays + binary search over maps for the index; maps are fine at the edges.
- Immutable snapshot + `atomic.Pointer` swap: readers never lock.
- Bounded top-K heaps instead of sorting everything.
- No reflection, `fmt.Sprintf` or `regexp` in hot paths; no allocation per comparison.
- `sync.Pool` only with a benchmark proving it helps.
- The domain stays pure, so it benchmarks without a database: keep it that way.
