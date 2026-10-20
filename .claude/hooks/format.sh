#!/usr/bin/env bash
# PostToolUse(Edit|Write): keep Go and templ files formatted and templ output fresh.
# Exit 2 hands the error back to Claude so it fixes the file it just wrote.
set -uo pipefail
f=$(jq -r '.tool_input.file_path // empty')
[ -f "$f" ] || exit 0
cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
case "$f" in
  *_templ.go) exit 0 ;;
  *.go)
    if ! out=$(gofmt -l -w "$f" 2>&1); then
      echo "gofmt failed on $f:" >&2; echo "$out" >&2; exit 2
    fi ;;
  *.templ)
    if ! out=$(go tool templ fmt "$f" 2>&1); then
      echo "templ fmt failed on $f:" >&2; echo "$out" >&2; exit 2
    fi
    if ! out=$(go tool templ generate -f "$f" 2>&1); then
      echo "templ generate failed on $f:" >&2; echo "$out" >&2; exit 2
    fi ;;
esac
exit 0
