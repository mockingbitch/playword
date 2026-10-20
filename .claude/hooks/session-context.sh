#!/usr/bin/env bash
# SessionStart: a few lines of live context (branch, pending changes, services).
set -uo pipefail
cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
branch=$(git branch --show-current 2>/dev/null)
dirty=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')
db=down; (exec 3<>/dev/tcp/127.0.0.1/55432) 2>/dev/null && db=up
web=down; curl -sf -m 1 http://127.0.0.1:8020/readyz >/dev/null 2>&1 && web=up
msg="Playword: branch ${branch:-?}, ${dirty} changed files, dev DB ${db} (127.0.0.1:55432), server ${web} (:8020)."
[ "$branch" = "master" ] || [ "$branch" = "main" ] && msg="$msg On ${branch}: create a feature branch before committing."
jq -n --arg m "$msg" '{hookSpecificOutput: {hookEventName: "SessionStart", additionalContext: $m}}'
