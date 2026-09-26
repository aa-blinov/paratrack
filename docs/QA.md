# QA

Status of the last full pass: **all four suites green**.

| Suite | Command | Result |
|---|---|---|
| Unit + integration | `go test ./...` | 6 packages ok |
| UI / visual | `python e2e/qa_full.py` | **63/63** |
| Business logic | `python e2e/qa_logic.py` | **65/65** |
| Offline + push | `python e2e/wave9_verify.py` | **14/14** |

Screenshots: `e2e/screenshots/qa/`, `e2e/screenshots/qa-logic/`, `e2e/screenshots/wave9/`.

---

## UI / visual matrix (`e2e/qa_full.py`)

| Block | Checks |
|---|---|
| A. Auth | register, login, logout, forgot-password reachable, lang switch on public pages |
| B. Timer | start, pause, resume, stop, backfill, active-list HTMX swap |
| C. Dashboard | active sessions, focus button, duration readout |
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

## Offline + push (`e2e/wave9_verify.py`)

| Check | Proof |
|---|---|
| Service worker | registered, scope `/`, active |
| Offline banner | appears on `offline` |
| Mutation queue | `POST /api/start` queued **with body** |
| Flush | queue drains on reconnect, session actually created |
| VAPID | `GET /api/push/key` returns `publicKey` |
| Push page | `window.paratrackPush`, status messages bound |
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
   inside `<script>`, which is a JS syntax error, so `window.paratrackPush` never existed.
   Messages moved to `data-msg-*` attributes.

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
