#!/usr/bin/env bash
# Stop: if Go or templ files changed, the code must at least build and vet
# before Claude hands back. Exit 2 keeps Claude working on the errors.
set -uo pipefail
input=$(cat)
[ "$(jq -r '.stop_hook_active // false' <<<"$input")" = "true" ] && exit 0
cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
changed=$(git status --porcelain -- '*.go' '*.templ' 2>/dev/null | awk '{print $NF}')
[ -z "$changed" ] && exit 0
if ! out=$(go build ./... 2>&1); then
  printf 'go build ./... fails after your changes — fix before finishing:\n%s\n' "$out" >&2
  exit 2
fi
pkgs=$(printf '%s\n' "$changed" | grep '\.go$' | xargs -r -n1 dirname | sort -u | grep -v '^\.scripts/' |
  while read -r d; do [ -d "$d" ] && printf './%s\n' "$d"; done || true)
if [ -n "$pkgs" ] && ! out=$(go vet $pkgs 2>&1); then
  printf 'go vet fails on the packages you changed — fix before finishing:\n%s\n' "$out" >&2
  exit 2
fi
exit 0
