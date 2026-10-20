---
description: Show what the rhyme engine returns for a word or phrase, from the dev database (no server needed)
argument-hint: "<word or phrase> [flags, e.g. -level L2 -dialect nam]"
allowed-tools: Bash(go run ./cmd/playword-cli *)
---
Run `go run ./cmd/playword-cli parse "<phrase>"` and `go run ./cmd/playword-cli search <flags> <phrase>` for:
$ARGUMENTS

(`search` builds the index from the dev database at `DATABASE_URL`, default the local one; see
`go run ./cmd/playword-cli search -h` for flags.) Show the parsed syllables and keys, then the top results
grouped by matched syllables and level, with their first meaning. Only report what the engine returned —
never add words of your own.
