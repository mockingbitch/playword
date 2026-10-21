---
paths:
  - "internal/domain/rhyme/**"
  - "pkg/vnphon/**"
  - "configs/**"
  - "internal/adapters/index/**"
---
# Rhyme domain — the product's core

- Syllable model: onset, medial, nucleus (`A AW AA E EE I O OO OW U UW IE UO UOW`), coda (`"" j w m n ng nh
  p t k ch`), tone 1–6. Levels nest: `L0 ⊂ L1 ⊂ L2 ⊂ L25 ⊂ L3`; English `perfect ⊂ family ⊂ assonance`.
- `Keyer.Key(level, syllable)` produces **equivalence classes** (cross pairs merged with union-find). Keep that
  property: anything that groups by key relies on transitivity. `codeLevel` must agree with key comparison
  (`TestCodeLevelMatchesKeys`).
- `configs/*.yaml` and `KeyerVersion` are part of `ConfigID`; changing either requires `rebuild-keys`, and
  the server refuses to serve keys built from another config (`/readyz` 503) — that is intended.
  **Every byte counts, comments included**: the hash is over the file text. Never touch these files in a
  bulk find-and-replace (a path rename once broke every environment's index this way); leave stale comments
  alone unless the change ships with `rebuild-keys`.
- Dialect rules are checked against Wiktionary regional IPA with `playword-cli dialect-check`; report the
  match rate before and after a change.
- **Ask before changing rules**: what rhymes, at which level, how groups form, which words are stopwords,
  dialect mappings, scoring order. Propose with examples from real lyrics, then wait for a decision.
- Scoring (`Score`) bounds keep "one more matched syllable" above everything else; keep the comment and the
  bound test in sync.
- Rhyme check (`check.go`): chains of ≥ 2 syllables, within a stanza, grouped strictest partner first, the
  stricter group wins overlaps, broken chains lose colour (no one-syllable fragments), repeats are marked not
  rhymed. Offsets are UTF-16 for the browser. See `.docs/plans/rhyme-check.md`.
