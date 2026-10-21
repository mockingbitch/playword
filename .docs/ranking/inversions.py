#!/usr/bin/env python3
"""Count ranking inversions in a TSV from `playword-cli search -queries`:
a stricter level ranked below a looser one inside the same tier — same
matched length and, when the TSV has them, the same proper-noun flag and the
same number of repeated words (both rank later on purpose, ADR-004)."""
import sys, collections
order = {"L0": 0, "L1": 1, "L2": 2, "L25": 3, "L3": 4, "perfect": 0, "family": 1, "assonance": 2}
rows = collections.defaultdict(list)
for line in open(sys.argv[1], encoding="utf-8"):
    f = line.rstrip("\n").split("\t")
    if f[0] == "query" or f[1] == "0":
        continue
    ident = int(f[6]) if len(f) > 6 else 0
    proper = int(f[8]) if len(f) > 8 else 0
    rows[f[0]].append((int(f[1]), f[2], int(f[3]), f[4], ident, proper))
total = 0
bad = []
for q, rs in rows.items():
    n = sum(1 for i, a in enumerate(rs) for b in rs[i + 1:]
            if a[2] == b[2] and a[4] == b[4] and a[5] == b[5] and order[b[3]] < order[a[3]])
    total += n
    if n:
        bad.append((n, q))
print(f"queries {len(rows)}, inversions {total}, queries with inversions {len(bad)}")
for n, q in sorted(bad, reverse=True)[:8]:
    print(f"  {n:4d}  {q}")
