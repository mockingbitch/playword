---
name: perf
description: Benchmark, profile and optimise a Playword hot path with before/after numbers, keeping the domain pure and tests green. Use when the user reports slowness, when a hot-path change needs proof, or to check the performance budgets.
argument-hint: "<package or function or endpoint>"
context: fork
agent: perf-engineer
---
Measure and, if worthwhile, improve the performance of: $ARGUMENTS

Follow .claude/rules/performance.md: baseline benchmark (`-benchmem -count 6`, realistic input) → pprof →
one change at a time → tests with `-race` → benchmark again. Return a before/after table (ns/op, B/op,
allocs/op), the profile evidence for each change, and the budget status. Revert changes that do not pay off.
