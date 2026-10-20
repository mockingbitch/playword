---
name: ui-change
description: Change Playword's UI (templ views, app.css, app.js) within the CSP, no-build, no-JS-fallback and accessibility rules, then verify it in a real browser at several widths and both themes. Use for any visual or interaction change.
argument-hint: "<what to change>"
---
# UI change: $ARGUMENTS

1. Read .claude/rules/frontend.md. Find the template (`internal/adapters/http/views/*.templ`) and the CSS
   section in `static/app.css`; reuse tokens and components (`.btn`, `.chip`, `.seg`, `.card`, `.empty`…).
2. Markup first, working without JavaScript (real links/forms). Then CSS. Then JS only for enhancement:
   event delegation on `document`, no inline handlers, `isComposing` guard on key handlers, errors handled.
3. Copy in Vietnamese; examples are live data, never invented words.
4. `make generate` (a hook does it per file) and `go test ./internal/adapters/http/...`; update assertions
   whose markup changed on purpose, golden files with `-update` after reading the diff.
5. Run the app (`make run` or a build on another port) and `make uicheck UICHECK_FLAGS='-paths <pages>
   -themes dark,light'`. Open the screenshots and look at 360, 768 and 1440 — overflow, clipped diacritics,
   contrast, focus, empty/error states. Or delegate to the `ui-auditor` agent.
6. Check CSS + JS stay under 30 KB gzip: `gzip -9c internal/adapters/http/static/app.{css,js} | wc -c`.
7. Report what changed and attach the screenshot paths that show it.
