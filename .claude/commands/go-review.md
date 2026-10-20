---
description: Review the current Go change with the go-reviewer agent
argument-hint: "[git range or paths, default: branch vs master + untracked]"
---
Use the `go-reviewer` subagent to review: $ARGUMENTS (when empty: the changes on this branch against master, including untracked files).
Then summarise its findings for the user in Vietnamese, most severe first, and ask which to fix.
