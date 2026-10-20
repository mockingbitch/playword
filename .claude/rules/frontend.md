---
paths:
  - "internal/adapters/http/views/**"
  - "internal/adapters/http/static/**"
---
# Frontend — templ + HTMX, no build step

- **Edit `.templ`, never `*_templ.go`** (a hook regenerates on save; CI checks they are fresh).
- **CSP** `default-src 'self'; style-src 'self'; script-src 'self'`: no inline `<script>`, no `on*=`
  attributes, no `style="…"` in markup, no CDN or remote fonts. Setting styles from JS via CSSOM
  (`el.style.x = …`) is allowed. New assets are vendored into `static/` with their licence.
- **Works without JavaScript**: every action is a real link or form (`action` + `method` + CSRF field);
  HTMX/JS enhance it. Handlers answer both: HTMX partial, or redirect 303 / full page.
- **Vietnamese input methods**: key handlers return early on `e.isComposing`; keep textareas real textareas
  (the rhyme-check mirror pattern), never `contenteditable` for lyrics.
- **Design tokens** live at the top of `static/app.css` (`--bg`, `--surface`, `--accent`, `--rc1…7` …) with a
  light theme under `prefers-color-scheme: light`. Use tokens, not raw colours. Brand: hip-hop dark, yellow
  highlighter accent, Be Vietnam Pro (vendored), uppercase 800 display type.
- **Colour that encodes data** (levels, rhyme groups) is validated with the dataviz validator and never the
  only cue: letters, labels or patterns go with it.
- **Responsive**: works from 320 px; tap targets ≥ 44 px; no horizontal overflow (`make uicheck`); grid items
  that may shrink get `min-width: 0`; the tab bar is the phone navigation.
- **Accessibility**: one `h1` per page, labels on every control, `aria-current` on nav, a single `role=status`
  for live announcements (never `aria-live` on a whole result list), visible focus, reduced motion respected.
- Vietnamese copy, sentence case except display headings; never invent words in examples — use live results.
- Budget: hand-written CSS + JS < 30 KB gzip.
