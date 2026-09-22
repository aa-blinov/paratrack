# Changelog

All notable changes to paratrack. Versions are tagged at meaningful
milestones — see https://github.com/aa-blinov/paratrack/releases.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **Per-activity goals**: `paratrack goal set/list/unset`, full CRUD
  via `/api/goals`. Dashboard widget shows progress bars per goal
  with live updates from active sessions. Period-aware windows
  (daily / ISO-week / monthly) computed in UTC.
- **Free-form tags**: `paratrack tag add/list/attach/detach`. Stats
  page has an inline "+ tag" input on every session row and a
  `/stats?tag=…` filter that narrows the breakdown, distribution
  and session list. New tags auto-create on first attach.
- **/goals and /tags management pages** with chip-based UIs, delete
  buttons and CLI hints.
- **ECharts graph** (5.5.1, vendored ~1 MB) replaces the hand-rolled
  SVG. Stacked hour-of-day bars, interactive legend chips that toggle
  series visibility, MutationObserver rebuilds the chart on theme
  change.
- **GitHub Actions workflow** (`.github/workflows/ci.yml`) runs
  `go vet`, `go test -race`, and the Playwright suite on every
  push and PR. Uploads screenshots as artefacts on e2e failure.
- **Makefile** + `scripts/setup_e2e.sh` for one-line build, run and
  e2e setup.
- **22 Go unit tests** for the db package (`internal/db/goals_test.go`,
  `internal/db/tags_test.go`) covering period-range math, goal CRUD
  validation, tag auto-create, batched tag hydration, FK cascade.
- **45 Playwright E2E checks** across dashboard, stats, graph,
  theme, keyboard, ECharts canvas + tooltip, theme-rebuild, CSV
  download, goals CRUD and tags CRUD + filter.

### Changed
- Stats page tables collapse to a card-list layout on phones via a
  pure-CSS `.responsive-collapse` rule keyed off `data-label`
  attributes on every `<td>`.
- Topbar + nav wrap on narrow screens; the theme button hides its
  AUTO/DARK/LIGHT text label on phones.
- Status badges gain tiny CSS-only play/pause glyphs.
- Activity palette drops pure red (`#ef4444`) so no activity colour
  collides with the destructive-action red of Stop / Delete.
- Light/dark parity: every visible element themed via CSS variables;
  `--accent` updated, focus-visible ring added,
  `prefers-reduced-motion` short-circuit.

### Fixed
- Inline-edit duration input recomputes `end_at` from `start_at`
  server-side, not in the browser.
- ECharts hover events no longer eaten by Alpine's reactive proxy
  (init runs imperatively from `alpine:initialized`).
- Tooltip on graph hour with no data correctly shows zeros, not
  the previous hour's values.

### Removed
- Legacy Python implementation (`track/`, `tests/`, `pyproject.toml`,
  `uv.lock`, `.ruff.toml`) deleted from the repo. The Go binary is
  the only supported runtime.

## Migration notes

The SQLite schema at `~/.track/track.db` is shared with the
original Python implementation. Existing databases get the
`UNIQUE(activity_id, period)` index on `goals` and any missing
columns via `applyMigrations` on first start — no manual step
required.

## Past highlights

* **Phase 3 (polish)** — duration-edit, theme toggle, SVG timeline.
* **Phase 2 (web)** — HTMX + Alpine.js embedded UI, all 11 API
  endpoints.
* **Phase 1 (CLI)** — natural-language time parser, `add`/`log`/
  `stats` commands.
* **Phase 0 (bootstrap)** — `go.mod`, modernc.org/sqlite, schema,
  `status` command.