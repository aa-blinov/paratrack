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
- **Multi-user collaboration** — local email + password auth (bcrypt, 30-day sessions, HttpOnly cookies), per-user personal team created at registration, owner/member roles, 7-day invite tokens, `/settings/team|members|invites|profile`, top-bar workspace switcher dropdown, last-owner-cannot-leave guard. `internal/teams` is the new home for team CRUD; `internal/auth` owns users + sessions.
- **Hard-break schema migration**: pre-auth `track.db` is renamed to `track.db.bak.<UTC>` on first launch; fresh schema with `users / auth_sessions / teams / memberships / invites` is created. No manual import step.
- **Multi-tenant data layer**: every domain row (activities, sessions, tags, goals) carries `team_id`; all db helpers take `teamID int64` as their first arg, supplied by the auth middleware via `teamID(r)`. CLI / tests can still pass `0` to opt out of scoping.
- **86 Go unit tests** + **47 Playwright E2E checks** covering dashboard, stats, graph, theme, keyboard, ECharts, CSV, goals, tags, auth gate, workspace switcher.

### Changed
- **Whole UI on DaisyUI v5.** Every page uses `card`, `btn`, `input`, `table`, `badge`, `alert`, `progress`, `stat`, `kbd` — replacing the hand-rolled `app.css` (deleted). Light/dark parity is now driven entirely by `data-theme`.
- **Case-insensitive activity and tag names.** `COLLATE NOCASE` on the `name` columns; inserts/lookups lowercase on the Go side. `Work`, `work`, `WORK` resolve to one row.
- Stats tables collapse to a card-list on phones via `.responsive-collapse`.
- Topbar + nav wrap on narrow screens; theme button hides its AUTO/DARK/LIGHT label on phones.
- Status badges get play/pause glyphs.
- **Monochrome UI**: per-activity rainbow palette dropped — `colorFor` now returns a neutral `#6b7280` for every name, the `.activity-mark` decorative left-edge bar is gone, and chart series render as a single grey. Activity identity is carried by the name alone.
- **Per-activity colour coding restored** — `colorFor` hashes the lower-cased name onto a curated 10-colour palette (indigo, sky, teal, emerald, lime, amber, orange, violet, purple, pink), so each new activity lands on a stable, distinguishable slot. The colour is applied to the activity name itself (`style="color: {{.Color}}"`) on dashboard / stats / goals lists and on the graph legend chips; chart series reuse the same colour, so chips and stacked bars share one cue. Pure red is reserved for destructive actions.
- **Primary CTAs toned to neutral** — Start / Set goal / Add are now `btn-neutral` (solid black) instead of indigo `btn-primary`. The Stats Distribution bar fill is `bg-base-content` so it matches the goals progress bars. Destructive actions (Stop / Delete) keep `btn-error`, and status pills keep their success / warning tint — colour now only signals action severity, never decoration.
- **`s.render` propagates `r`**: every auth-gated page now calls the auth-aware renderer (`renderPageForRequest`), so the top-bar `{{if .User}}` branch actually picks up the user menu + workspace switcher on dashboard, stats, graph, tags, and goals — not only on the settings routes.
- Light/dark via CSS variables; focus-visible ring; `prefers-reduced-motion` short-circuit.
- Toast is a solid colored alert with a glyph (✓/✕).

### Fixed
- Inline-edit duration recomputes `end_at` server-side.
- ECharts hover events no longer eaten by Alpine's reactive proxy.
- Tooltip on empty graph hour shows zeros, not the previous hour's values.
- `goals.team_id` is now `NOT NULL DEFAULT 0` so the `UNIQUE(team_id, activity_id, period)` upsert actually detects duplicate goals (NULLs treated are treated as distinct otherwise).
- All db helpers (`CreateActivity`, `ListActivities`, `GetOrCreateActivity`, `CreateSession`, `CreateClosedSession`, `ListActiveSessions`, `ListClosedSessionsInRange`, `CreateTag`, `GetTagByName`, `ListTags`, `AttachTag`, `DetachTag`, `SetTagsForSession`, `ListSessionsByTag`, `ListAllTagsWithCounts`, `UpsertGoal`, `ListGoals`, `DeleteGoal`, `ProgressForGoals`, `TagsForSessions`) now take a `teamID int64` first arg. Pass 0 to skip the team scope (legacy / tests); the auth middleware always supplies the real id via `teamID(r)`. `goals.team_id` UNIQUE was widened to (team_id, activity_id, period) so per-team goals don't collide.
- `tags` table gains `UNIQUE (team_id, name)` so `CreateTag`'s `ON CONFLICT(team_id, name) DO NOTHING` actually resolves and the existing tag is returned (was failing with 400 before).
- `clipSeconds` rounds sub-second overlaps up with `math.Ceil`, so a session that literally just started (or one whose end was clamped to "now") no longer disappears from `/stats` due to `int(0.1s) = 0` truncation.

### Removed
- Legacy Python implementation deleted. Go binary is the only runtime.

## Migration notes

**Hard break on first launch.** A pre-auth `track.db` is renamed to `track.db.bak.<UTC-timestamp>` and a fresh schema is created. No manual import step. This is intentional — the auth model assumes a clean user/team table.

**Multi-tenant queries.** Every web request hits the middleware, which sets `User` and `Team` on the request context; db calls pass `teamID(r)` so cross-team reads are impossible by construction. The CLI is intentionally team-scope-free (passes `0` everywhere) and is therefore read-only-equivalent to a single-user legacy session — fine for personal scripts, not safe for shared hosts.

Case-insensitive rollout: any pre-existing activity or tag rows colliding under `COLLATE NOCASE` (e.g. `Work` + `work`) are merged — the lowest-id row wins, sessions / session_tags are re-pointed at the winner, losers are deleted. Idempotent.

## Past highlights

* **Phase 3 (polish)** — duration-edit, theme toggle, SVG timeline.
* **Phase 2 (web)** — HTMX + Alpine.js embedded UI, 11 API endpoints.
* **Phase 1 (CLI)** — natural-language time parser, `add`/`log`/`stats`.
* **Phase 0 (bootstrap)** — `go.mod`, modernc.org/sqlite, schema, `status` command.
