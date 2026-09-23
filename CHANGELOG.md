# Changelog

All notable changes to paratrack. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- **DaisyUI v5 + Tailwind v4 CSS pipeline** in `web/`. `make ui` (or `make build`) installs npm deps and produces `internal/web/static/css/paratrack.css` (~16 KB minified, embedded via `go:embed`). Two custom themes: `paratrack-light` (default) and `paratrack-dark`.
- **Per-activity goals**: `paratrack goal set/list/unset` + `/api/goals`. Dashboard widget with live progress; period windows (daily / ISO-week / monthly) computed in UTC.
- **Free-form tags**: `paratrack tag add/list/attach/detach`. Inline "+ tag" input on stats rows; `/stats?tag=…` filter. Auto-create on first attach.
- **/goals and /tags management pages** with chip UIs and CLI hints.
- **ECharts graph** (vendored) — stacked hour-of-day bars, clickable legend, MutationObserver rebuilds on theme change.
- **GitHub Actions** (`.github/workflows/ci.yml`) — three jobs: `ui` (npm), `unit` (Go), `e2e` (Playwright). Uploads screenshots on failure.
- **48 Go unit tests** + **45 Playwright E2E checks** covering dashboard, stats, graph, theme, keyboard, ECharts, CSV, goals, tags.
- **3 colour tests** (`TestColorForReturnsNeutralGrey`, `TestColorForDeterministic`, `TestColorForCaseInsensitive`) pinning the monochrome `colorFor` contract.

### Changed
- **Whole UI on DaisyUI v5.** Every page uses `card`, `btn`, `input`, `table`, `badge`, `alert`, `progress`, `stat`, `kbd` — replacing the hand-rolled `app.css` (deleted). Light/dark parity is now driven entirely by `data-theme`.
- **Case-insensitive activity and tag names.** `COLLATE NOCASE` on the `name` columns; inserts/lookups lowercase on the Go side. `Work`, `work`, `WORK` resolve to one row.
- Stats tables collapse to a card-list on phones via `.responsive-collapse`.
- Topbar + nav wrap on narrow screens; theme button hides its AUTO/DARK/LIGHT label on phones.
- Status badges get play/pause glyphs.
- **Monochrome UI**: per-activity rainbow palette dropped — `colorFor` now returns a neutral `#6b7280` for every name, the `.activity-mark` decorative left-edge bar is gone, and chart series render as a single grey. Activity identity is carried by the name alone.
- **Primary CTAs toned to neutral** — Start / Set goal / Add are now `btn-neutral` (solid black) instead of indigo `btn-primary`. The Stats Distribution bar fill is `bg-base-content` so it matches the goals progress bars. Destructive actions (Stop / Delete) keep `btn-error`, and status pills keep their success / warning tint — colour now only signals action severity, never decoration.
- Light/dark via CSS variables; focus-visible ring; `prefers-reduced-motion` short-circuit.
- Toast is a solid colored alert with a glyph (✓/✕).

### Fixed
- Inline-edit duration recomputes `end_at` server-side.
- ECharts hover events no longer eaten by Alpine's reactive proxy.
- Tooltip on empty graph hour shows zeros, not the previous hour's values.

### Removed
- Legacy Python implementation deleted. Go binary is the only runtime.

## Migration notes

Existing databases get the `UNIQUE(activity_id, period)` goals index and any missing columns via `applyMigrations` on first start. No manual step.

Case-insensitive rollout: any pre-existing activity or tag rows colliding under `COLLATE NOCASE` (e.g. `Work` + `work`) are merged — the lowest-id row wins, sessions / session_tags are re-pointed at the winner, losers are deleted. Idempotent.

## Past highlights

* **Phase 3 (polish)** — duration-edit, theme toggle, SVG timeline.
* **Phase 2 (web)** — HTMX + Alpine.js embedded UI, 11 API endpoints.
* **Phase 1 (CLI)** — natural-language time parser, `add`/`log`/`stats`.
* **Phase 0 (bootstrap)** — `go.mod`, modernc.org/sqlite, schema, `status` command.
