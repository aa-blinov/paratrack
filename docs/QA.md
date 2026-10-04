# QA

## Automated CI gates

GitHub Actions is the source of truth for pushes and pull requests targeting
`main` or `master`. It runs these gates:

| Job | Commands / coverage |
|---|---|
| UI build | `npm ci`, `npm run build`, `npm run typecheck`, `npm run test:unit`, `npm run test:js` |
| Architecture | `make verify` (Go static/security/build checks, architecture rules, JS lint/import graph and npm audit) |
| Go tests | `go test -race -count=1 -p 1 ./...` plus coverage for `internal/db`, `internal/web` and `internal/timeparse` |
| Browser E2E | `python e2e/test_dashboard.py` against the CI Postgres service |

The expanded QA suites below are manual acceptance runs. Their counts are the
last recorded snapshot and do not prove the current working tree or a specific
commit; use the CI run for commit-specific status.

## Last recorded manual QA snapshot

| Suite | Command | Result |
|---|---|---|
| Unit + integration | `go test ./...` | 6 packages ok |
| UI / visual | `python e2e/qa_full.py` | **63/63** |
| Business logic | `python e2e/qa_logic.py` | **65/65** |
| Offline + push | `python e2e/qa_offline_push.py` | **14/14** |

Screenshots: `e2e/screenshots/qa/`, `e2e/screenshots/qa-logic/`, `e2e/screenshots/offline-push/`.

Mutating QA scripts default to the local server at `http://127.0.0.1:8888`.
Start the local service with `make e2e-up` before running them. Set
`PARATRACK_BASE` explicitly when intentionally targeting another deployment.

---

## UI / visual matrix (`e2e/qa_full.py`)

| Block | Checks |
|---|---|
| A. Auth | register, login, logout, forgot-password reachable, lang switch on public pages |
| B. Timer | start, pause, resume, stop, backfill, dashboard refresh |
| C. Dashboard | active sessions, focus button, duration readout |
| C2. React projects | project list cards/archive filter; create form submits through the existing handler; detail history and edit form submit through existing handlers |
| C3. React goals | create and delete through existing API; progress refreshes from the progress endpoint |
| C4. React tags | create/delete controls; tag list returns session usage counts |
| C5. React graph | ECharts canvas, scoped period links, responsive legend toggle and theme redraw |
| C6. React timesheet | week navigation, responsive grid and cell updates refresh row/day/week totals |
| C7. React payroll | pay-run form/overlap confirmation; detail status, print and delete actions |
| C8. React schedule | week navigation and project selection; role-aware editable cells autosave and refresh row totals; no-project and member empty states |
| C9. React invoices | unbilled and unassigned activity; project selection prefills client details; invoice generation; detail status, PDF, print, email and receipt actions |
| C10. React stats | period/person/project/tag filters preserve scope; saved reports; project/activity totals; inline session edits, tag changes and delete refresh the filtered view |
| C11. React reports | report template gallery; date range form; result totals and billable columns; CSV export and print actions |
| C12. React export | session CSV date-range validation; summary report navigation respects permissions and workspace modules |
| C13. React integrations | provider-specific credential hints; connect and delete forms; marketplace availability/connected states; task sync and timer start actions |
| C14. React API tokens | expiry and read-only creation options; raw token shown once with copy action; existing-token metadata and delete actions |
| C15. React profile settings | profile name and password forms retain their existing endpoints and validation; disabled email; success and error flashes |
| C16. React personal preferences | duration, week start, time zone, default project, dashboard blocks, section visibility and four-tab limit save through the existing preferences endpoint |
| C17. React notifications | browser subscription state; subscribe/unsubscribe via existing push APIs; permission, unavailable browser and request failure messages; device count and event list |
| C18. React workspace settings | rename, currency, invoice requisites, billing rounding and logo, Stripe credentials, workspace creation and deletion forms preserve the existing routes |
| C19. React members | role, pay, capacity, remove-member and owner transfer forms remain role-aware and submit through the existing handlers |
| C20. React invites | create with optional email delivery; live links, used/expired state, and revoke actions |
| C21. React sections and welcome | presets, custom module selection, manager-only section behavior and onboarding skip/persist paths |
| C22. React webhooks | endpoint creation/deletion, event subscriptions and five latest delivery results |
| C23. React audit | latest 100 workspace events, translated action names and empty state |
| C24. React import | provider-specific fields/hints, preview list, timezone capture and confirmation run retain existing import handlers |
| C25. React help | all translated feature guides, keyboard shortcuts and settings links |
| C26. React invite acceptance | invalid, expired, used, anonymous and signed-in states; accept and logout actions retain existing routes |
| C27. React authentication | login/register and password recovery/reset forms keep CSRF, redirect target, SSO link, validation and error/info states |
| D. Stats | period tabs, project/tag filters, saved-reports chips, session rows |
| E. Graph | ECharts canvas, series, themed tooltip |
| F. Tags | create, attach, delete (confirm dialog) |
| G. Projects | create, estimate card, billable rate |
| H. Timesheet | week grid renders, cell edit |
| I. Schedule | people×week grid, cell edit |
| J. Invoices | generate, number, PDF download, manual payment link, Mark paid |
| K. Payroll | create run, number |
| L. Reports | 4 templates run |
| M. Integrations | providers list, connect form |
| N. Tokens | create, raw shown once, delete |
| O. Webhooks / audit | create webhook, audit rows |
| P. i18n / theme | RU + EN dashboard, toast position bottom-right |
| Q. PWA | manifest + service worker registered |
| R. API v1 / Bearer | summary + sessions with `pt_` token |
| S. Mobile | no horizontal overflow on `/`, `/stats`, `/invoices` |

## Business-logic matrix (`e2e/qa_logic.py`)

| Block | Proven math |
|---|---|
| A. Timer | backfill `09:00→11:30` = **9000 s** in `/api/v1/sessions` |
| B. Invoices | 2 h 30 m × 40.00/h = **100.00**, `INV-…`, PDF `%PDF` |
| C. Timesheet | cell 120 m → **2h**; cell 0 clears the day |
| D. Estimates | 600 m = **10h** / 2h tracked |
| E. Payroll | `PAY-YYYY-NNN` from tracked time × 100.00/h |
| F. Reports | 5 templates render; CSV `key,hours,seconds,rate_cents,amount_cents,share` |
| G. Marketplace | 11 cards |
| H. API | `pt_` bearer works on `/api/v1/*`; bad token → 401 |
| I. Webhooks / audit | webhook created, audit rows present |
| J. Security | `DENY` / `nosniff` / CSP / HSTS; bad CSRF → 403; login → **429** after 10 |
| K. i18n | RU + EN switch |
| L. CSV | `text/csv` export with data rows |

## Offline + push (`e2e/qa_offline_push.py`)

| Check | Proof |
|---|---|
| Service worker | registered, scope `/`, active |
| Offline banner | appears on `offline` |
| Mutation queue | `POST /api/start` queued **with body** |
| Flush | queue drains on reconnect, session actually created |
| Sensitive mutations | login, password, integration, push and payment bodies are not queued |
| VAPID | `GET /api/push/key` returns `publicKey` |
| Push page | controls and status are present; behavior stays module-scoped; translated messages are bound |
| Enable push | status updates (no silent click) |
| Subscribe / unsubscribe | 200 |
| PWA manifest | `display: standalone`, 2 icons |
| Page errors | none |

---

## Bugs found and fixed in this pass

1. **`/projects` returned 500** — `{{.T}}` inside a `{{range}}` on `projectListRow`
   (`can't evaluate field T`). Switched to `{{$.T}}`.
   *Class: template range + missing `T()` on the row type.*

2. **Same-day invoices and pay runs were rejected** (`bad period`) — the check was
   `end.After(start)`, so `start == end` failed. Now only `end.Before(start)` is an error;
   the exclusive-end expansion below already turns a single day into `[start, start+1d)`.

3. **Unassigned (no-project) time landed on invoices at rate 0** — `COALESCE(p.billable, 1)`
   made a NULL project look billable. Now rows without a project are skipped.

4. **`/api/v1/sessions` omitted still-running sessions** — it only queried
   `ListClosedSessionsInRange`, so a client could `POST` a session and never `GET` it back.
   Active sessions in the window are now appended.

5. **Offline queue dropped request bodies and treated HTTP 400 as success** — a replayed
   `POST /api/start` sent only `csrf_token` and was dequeued anyway. The queue now stores
   the body and only dequeues on 2xx.

6. **Push settings silently did nothing** — `html/template` double-escapes `{{printf "%q"}}`
   inside `<script>`, which is a JS syntax error. Messages moved to `data-msg-*`
   attributes and behavior now lives in an ES module without a `window` global.

7. **Service worker had scope `/static/`** — a script at `/static/sw.js` cannot control `/`.
   Now served at `/sw.js` with `Service-Worker-Allowed: /`.

---

## Contract notes for test authors

These are real field names — wrong ones silently fail with 303 + flash:

| Endpoint | Fields |
|---|---|
| `POST /api/start` | `activity` (**not** `name`) |
| `POST /api/sessions/backfill` | `activity`, `start`, `end`, `note` |
| `POST /api/timesheet/cell` | `activity_id`, `date`, `minutes` |
| `POST /projects/new` | `name`, `slug`, `color` |
| `POST /projects/{slug}` | `name`, `color`, `estimate_minutes`, `rate_cents`, `billable` |
| `POST /invoices` | `client`, `start`, `end`, `notes`, `project_id` |
| `POST /payroll` | `start`, `end`, `notes` |
| `POST /api/member/pay` | **`user_id`**, `hourly_pay_cents`, `capacity_minutes` |
| `POST /api/activities/{id}/project` | **numeric** `project_id` (slug → 400) |

Other traps:

- Money and multi-digit numbers are split across HTML tags — assert on stripped text
- `html/template` + `{{printf "%q"}}` inside `<script>` is double-escaped — use data attributes
- A live timer on the same activity joins the next invoice; stop it or use a dedicated activity
- `locator("form").first` on paratrack pages is the hidden workspace-switcher
- Playwright `page.request.*` does not send `X-CSRF-Token`
