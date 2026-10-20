---
description: Run Playword's quality gate (make check) and fix what fails
allowed-tools: Bash(make check), Bash(make generate), Bash(go *), Bash(gofmt *), Read, Edit
---
Run `make check`. If it fails, read the first failure, fix the cause (not the symptom; never delete or loosen
a test), and run `make check` again until it passes. Report in Vietnamese: what failed, what you changed,
the final result.
