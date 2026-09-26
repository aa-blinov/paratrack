# Changelog

All notable changes to paratrack. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed — typography & icons
- **Type scale restored**: DaisyUI 5 resets `h1..h6` to `font-size: inherit`, which flattened the whole UI to one size. A minor-third scale (`--step--2` … `--step-4`) is now defined on `:root` and applied to headings, card titles, labels and meta.
- **UI font is Inter** (variable, latin + latin-ext + cyrillic) replacing Manrope. Body is pinned to 16px with `font-optical-sizing: auto`.
- **Logo wordmark is Fraunces** (variable "full" cut with SOFT/WONK axes) — used only for `.wordmark` "paratrack", with a Lucide `timer` glyph beside it. JetBrains Mono stays for code, kbd and timestamps.
- **Lucide icon set** (ISC) vendored as an SVG sprite at `/static/icons.svg` (35 symbols) and exposed as `{{icon "play"}}`. Replaces the hand-drawn ▶/⏸/☰/● glyphs in nav, status pills and session actions (Focus/Pause/Resume/Stop/Start/Add).

### Added — production SaaS hardening
- **CSRF double-submit** on every state-changing request. A readable `paratrack_csrf` cookie is echoed via hidden form fields and the `X-CSRF-Token` header (HTMX gets it from `htmx:configRequest`, `fetch` from `paratrackCSRF()`). Missing/incorrect token → 403.
- **Security headers** on every response: `Content-Security-Policy` (self + the inline/eval relaxations Alpine 3 requires), `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, `Permissions-Policy`, and HSTS when the request arrived over TLS.
- **Secure cookies** — `Secure` is set on session / team / CSRF cookies when the request is HTTPS (direct TLS or `X-Forwarded-Proto: https`).
- **Auth rate limiting** — in-memory sliding window per IP: login 10/min, register 5/min, password-forgot 5/min, password-reset 10/min. 429 with `Retry-After`.
- **Password recovery** — `/forgot-password` + `/reset-password`, single-use 30-minute tokens (`password_reset_tokens`), "if that address exists" responses (no enumeration), successful reset logs out every other session. Mail goes through a pluggable `internal/mail.Sender`: log sink by default, SMTP relay via `PARATRACK_SMTP_HOST` / `PARATRACK_SMTP_USER` / `PARATRACK_SMTP_PASS` / `PARATRACK_MAIL_FROM`.
- **Web backfill** — "Add a past session" card on the dashboard (`POST /api/sessions/backfill`), natural-language times via the same parser as `paratrack add`.
- **Focus control in the UI** — per-row Focus button on active sessions (and a "Focus first" shortcut) calling `POST /api/focus/{name}`.
- Toast sits under the topbar (no longer covers the nav).
- Tests: `saas_test.go` (CSRF reject, security headers, full reset flow with spy mailer, rate-limit 429, backfill) and `rl_test.go` (limiter window accounting).

### Fixed
- **Stop folds live elapsed into `accumulated_seconds`**, so a closed session's `DurationSeconds` is no longer 0 for never-paused rows and correctly excludes pause gaps for paused ones.
- **Tracked time is pause-aware and window-scaled everywhere**: `Session.TrackedSecondsInWindow` attributes a session's non-paused total to a period by its wall-clock overlap fraction. Goals (both active and closed), `/stats` aggregates and project card windows all use it — previously goals counted live sessions as accumulated but closed ones as wall-clock (including pauses).
- **`DeleteTag` is team-scoped** — a signed-in user could previously delete any workspace's tag by numeric id. Missing goal/tag now map to 404 (was 500).
- **Names in query strings are `urlquery`-escaped** on goals delete, session tag detach, and `/stats?tag=` links — "deep work" used to produce a broken URL.
- **CSV `project` column carries the project name** (was slug) and `duration_seconds` is the tracked duration (was wall-clock span).
- **`POST /api/start` fails cleanly when the chosen project can't be assigned** (was silently starting the session uncategorized).
- **Inline tag attach actually updates the row**: the `+ tag` input was missing `hx-target` / `hx-swap`, so HTMX dumped the `session-row` response into the `<input>` and the chip never appeared (toast still said "tagged"). Now Enter and the new `+` button both replace the row.
- **Inline duration edit no longer kills row bindings**: `paratrackResize` injects HTML outside HTMX and must call `htmx.process()`, otherwise tag/pause/delete stops working after an inline edit.
- **Mobile header no longer overflows the viewport** (was ~723px at 390px width): primary nav collapses into a hamburger menu under `sm`, workspace name truncates, period tabs scroll horizontally instead of stretching the document. `ui_audit.py` asserts `scrollWidth == clientWidth` on dashboard/stats/goals.
- **Duration format unified** to `Xh Ym` / `Xm` / `Xh` everywhere (stats, goals, projects cards, graph total, live ticker). The template `fmtDuration` no longer uses the zero-padded `%dh %02dm` ladder; the Alpine live ticker no longer emits `HH:MM:SS`.
- **Dashboard and tag-filter totals** no longer collapse to zero: aggregates read `sessionView.DurationSecs` instead of parsing the human label with a `HH:MM:SS` parser (`parseHMSStrict` removed).
- **Graph "Total tracked"** was 60× off (minutes fed into a seconds formatter).
- **Tag filter now scopes the whole /stats page** — breakdown, distribution and totals all describe the same row set (previously the breakdown ignored `?tag=`).
- **HTMX response contract**: `POST/DELETE /api/tags` and `/api/goals` return the `tags-list` / `goals-list` fragments under `HX-Request`; session tag attach/detach return the re-rendered `session-row` (they used to return an empty body that wiped the row on `outerHTML` swap). Non-HTMX callers keep the JSON/200 shapes.
- **Inline session edit keeps tags + project badge** (`handleUpdateSession` now hydrates before re-rendering the row).
- **Goals and Tags pages** ship a real `<title>` and nav highlight (`render()` no longer drops Title/Active for those view-models).
- **Dashboard "Recent sessions (last 7 days)"** actually queries 7 days; empty state no longer links to a nonexistent "log a past session" action.
- **/stats period tabs preserve `?project=`**; `/graph` gained the missing "Last month" tab; the filter banner reflects both project and tag.
- **Project detail totals**: "Last 30 days" uses clipped duration (was `accumulated_seconds`), "All time" is computed and rendered (was always `0m`). Project card windows compare timestamps via `db.FormatTime` (RFC3339Nano) instead of a mismatched local format.
- **Invite revoke form** works (`POST /api/team/invites/{token}/revoke` added next to `DELETE /api/team/invites/{token}`).
- **Register preserves `?next=`** so the invite-accept → sign-up → join flow lands on the invite.
- **Invite-accept "log out"** is a POST form, not a GET link to a POST-only route.
- **Delete workspace** has a Danger-zone control; deleting one of several workspaces switches to the next instead of logging the user out. `next` on workspace switch is validated as a relative path (open-redirect close).
- **Password change requires the current password** (was skippable by leaving the field empty).
- **Session mutations are team-scoped**: `GetSession` / `UpdateSessionEnd` / `PauseSession` / `ResumeSession` / `DeleteSession` / `AttachTag` / `DetachTag` take a `teamID` and refuse cross-workspace access. Previously any signed-in user could mutate any session by numeric id.
- **Primary CTAs on /projects** toned to `btn-neutral` to match the rest of the app.
- Slug input `pattern` made a valid HTML5 regular expression (the `-` placement broke the `/v` unicode-classes parser in Chromium).

### Added
- Regression tests for the duration ladder, `DurationSecs`, graph total units, tag filter, `pageMeta` coverage, HTMX/JSON dual shape, and cross-team session isolation.
- **API contract suite** (`internal/web/api_contract_test.go`) covering session lifecycle + pause-aware CSV durations, goals HTMX-fragment vs JSON shapes with spaced names, tag team-scope deletes, CSV project-name column, duplicate-start 409, and inline duration edit round-trip.

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
