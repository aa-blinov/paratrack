# paratrack

Minimalist time tracker with parallel activities, team workspaces and billing.
Single Go binary, embedded web UI, Postgres. No Node at runtime, no CGO.

**Live:** https://paratrack.duckdns.org

---

## 1. What it does

| Area | Capability |
|---|---|
| **Timer** | Start / pause / resume / stop parallel activities, inline edit of start, end and duration |
| **Backfill** | Add a past session in natural language (`yesterday 09:00` → `11:30`) |
| **Timesheet** | Week grid (activity × days), cell is the source of truth |
| **Projects** | Colored projects, estimates vs actual, billable rate |
| **Tasks** | Board and table per project or workspace. A task has a status, an assignee, a due date and an estimate, and holds many activities: time logged to any of them counts on the task. Archiving takes it off the board, stops a timer running on its activities and keeps the time; no new time is logged until it is restored. A GitHub Projects v2 board imports once and then follows: each item keeps a link to its issue or pull request (`github_projects.go`) |
| **Stats / Graph** | Breakdown by project and activity, distribution, ECharts graph, CSV export |
| **Goals** | Daily / weekly / monthly minute targets per activity |
| **Tags** | Free-form labels on sessions, filterable |
| **Invoices** | `INV-YYYY-NNN`, draft → sent → paid, PDF, online payment link |
| **Payroll** | `PAY-YYYY-NNN` pay runs from tracked time × member rate |
| **Schedule** | Per person, a line per project in hours by day; load % against available time (capacity for five days less admin time); managers also see the time each person logged |
| **Integrations** | 8 providers (GitHub, GitLab, Jira, Trello, Asana, ClickUp, Todoist, Notion) + marketplace |
| **API** | `pt_` bearer tokens, `/api/v1/*`, webhooks with HMAC, audit log, OIDC SSO |
| **Clients** | MV3 browser extension, PWA (installable), offline queue + web push |
| **i18n** | Russian by default + English, switchable per user |
| **Import** | Toggl / Harvest / Clockify |

---

## 2. Business logic

### Tracked time

"Duration" is **non-paused** tracked time, never wall-clock span.

- Active session: `accumulated_seconds + live since last_resume_at`
- Closed session: `accumulated_seconds`
- Window clipping scales tracked time by the wall-overlap fraction of the window

Any inline edit of `duration` or `end_at` also writes `accumulated_seconds` — the hand-edit
defines the tracked total and discards pause history by design.

### Timesheet write model

A grid cell is the source of truth for that activity/day.
`UpsertDayTotal` deletes the day's closed sessions for the activity and inserts **one**
synthetic `note="timesheet"` session anchored at **09:00**. Minutes `0` clears the day.

### Approved weeks

A manager (owner or admin) approves a finished Monday-to-Sunday week of the timesheet
(`POST /api/timesheet/approve`, `POST /api/timesheet/reopen`; the form sends `start` and `end`
as the week's first and last day in the viewer's location). The approved window is
`[start 00:00, end+1 00:00)` in that location, stored as UTC instants in
`timesheet_approvals(team_id, starts_at, ends_at, approved_by, approved_at)`, unique per
`(team_id, starts_at)`. Approving the same window again changes nothing.

Refusals, each as its own error:

- `ErrWeekNotFinished`: the week has not ended.
- `ErrWeekHasRunningTimer`: a timer that started before the week's end is still running.
- `ErrWeekPaid` (reopen only): a pay run's period overlaps the week.
- Members get `403` on both routes.

While a window is approved, any write whose session **start** falls inside it returns
`SessionApprovalLockError`: session edit, delete, stop, reopen, a backfilled or added closed
session, a timesheet cell and a row clear. The check runs in the same transaction as the
write. The start decides because the grid files a session under the day it starts on.
Managers reopen a week to change it; the owner is not exempt. The refusal names the week by its first and last day and says that the owner or an admin can reopen it. The same sentence reaches the editor, the backfill, the timesheet cell and the `/api/v1` `409`.


### Money

All money is **integer cents**. `formatMoney(cents)` → `"123.45"`.

**Invoices**

- Project carries `billable_rate_cents` (hourly) and a `billable` flag
- Non-billable projects **never** land on an invoice
- Unassigned (no-project) time is **not invoiceable**
- Lines group tracked time by (project, activity)
- `amount_cents = seconds × rate / 3600` (truncated to the cent)
- The rate is snapshotted onto the line at creation time
- Invoice period `start`/`end` are dates; `end` is exclusive (`2026-09-25`–`2026-09-25` = one day)
- A draft carries an optional payment due day (`invoices.due_date`, `YYYY-MM-DD`, empty = none). It is set with the draft details and frozen when the invoice is sent (FIN-07)
- A sent invoice still unpaid after its due day is overdue: the list and the invoice page mark it. The due day itself is not late, a draft is never overdue, and a paid invoice is never overdue
- A sent invoice takes payments into a journal (`invoice_payments`: date, amount, note, source). It is paid when the journal covers the total; marking it paid books what is still owed, so the journal always adds up to the total. A partial payment leaves it sent and shows how far it got. The journal is only ever added to, and it is exported with the workspace (FIN-08)
- The invoice list filters by status (overdue included), by the day it was issued, and by "not paid" (FIN-09). Without SMTP the invoice letter can still go out as an `.eml` file with the PDF inside (FIN-10)
- An invoice whose period holds billable time in a week the timesheet has not approved is not created at once: the form names those weeks and the manager confirms, and only then is the draft written (D-3, FIN-12). Payroll still counts approved weeks only

**Payroll**

- Member carries `hourly_pay_cents` (0 = not on payroll) and `capacity_minutes` (default 480)
- Runs skip members with pay 0
- Sessions attribute via `sessions.user_id` (stamped on timer start)
- Legacy rows without a user fall on the team's first paid member
- A run counts **only time inside approved weeks**. A closed session in the period that starts
  outside every approved window refuses the run with `PayrollNotApprovedError`, which carries the
  count and the first and last start; the message names them. The current week can be paid only
  after it has ended and been approved. `/payroll` lists the finished, unapproved weeks with tracked
  time from the last eight, each linking to its timesheet, with a button that approves that week
  from here (the same approval the timesheet makes; the form sends the Monday and the Sunday).

**Schedule**

- `schedule_entries` per (team, user, project, day), cell minutes, 0 clears
- `Available = capacity × 5 − admin`: the week's time for clients. `Load% = planned / available`, and `Tracked%` is the same ratio over the time the week's sessions logged, open timers included. A row says `overloaded` when planned time is over available time, and `idle` when it is strictly under half of a positive available time; overload takes precedence, and with no available time neither word appears. Admin time is set per member by a manager and never below zero available.
- Copying a week (`POST /api/schedule/copy`) is allowed only into an empty week from a week with a plan; otherwise it is refused, never merged.
- A project has an optional deadline (`projects.deadline`, YYYY-MM-DD) and an optional lead (`projects.lead_user_id`, a current member). The lead writes plan cells on that project only (`UpsertScheduleEntry` checks the lead, and membership, in the same transaction) and sees tracked time by person on the project page. Only managers change the deadline and the lead.
- Weekly norm (PLAN-02): a project may carry a norm in hours a week (`project_weekly_norms`). The project page lists six weeks around today with the plan and, where the viewer may see it, the tracked time. A week more than a tenth off the norm is marked. A past week is judged by its tracked time where the viewer may see it, and by the plan otherwise
- Reminders (`internal/reminders`, once an hour): a deadline within three days goes to the managers and the lead, once per deadline (`projects.deadline_reminded`); a person planned past their available time this week goes to the managers, once per person and week (`schedule_overload_reminders`). Topics `project.deadline`, `schedule.overload` and `task.due` can be muted. When SMTP is configured, the overload reminder also goes by email to the same managers, with the same mute rule (TIME-14). A task's assignee gets a nudge when its due date is today or tomorrow, and another when it is overdue: once per due date, by push and, with SMTP, by email (TASK-05).
- Managers see tracked time; a member sees only the plan.
- The project page shows the plan once, in the total row under "By person" (labelled "Total, all time"). That total is the sum of the person rows, and its tracked cell is the sum of the tracked column, so it appears only where that column does. A project nobody has planned shows no table and no total row. The remaining-estimate figure counts the plan from today onward and says so.
- The team utilization report (`/reports`, `utilization`) puts each member's planned minutes over the period beside their tracked time. A member with a plan and no tracked time gets a row with zero time. Without an explicit period it covers the current week, so the days still to come count toward the plan. Its load column is tracked time over planned time for the same days, empty when there is no plan. That is not the Schedule's Load%, which is planned over available.

### Sections and onboarding

A workspace picks its sections (`teams.modules`). An **empty** setting means every section, which
is what workspaces made before sections existed keep.

A workspace made new (registration, «Создать пространство», restoring a personal workspace after
the last one was deleted) starts with `teams.sections_chosen = 0`. While it is 0, every screen
redirects to `/welcome`. Open while unchosen: `/welcome`, `/help`, `/settings/*`, `/invites/*`
(so an account can always reach the way out). The owner chooses on `/welcome`: a preset (`solo`,
`freelance`, `studio`), «Пропустить» (which saves `solo`), or a custom list («Сохранить свой набор»,
posted with `from=welcome`). Saving any choice sets `sections_chosen = 1`. The column defaults to 1,
so existing workspaces keep their menu.

The `tasks` section is first in the catalogue and in every preset. A workspace with an explicit set
gets `tasks` appended once at start-up (`schema_compat.go`); one that never chose (empty) or chose
`none` is left alone.

### Numbers

| Format | Shape |
|---|---|
| Invoice | `INV-YYYY-NNN` |
| Payroll run | `PAY-YYYY-NNN` |
| API token | `pt_` + 32 random bytes (hex), stored as SHA-256 |
| Money | integer cents, rendered `123.45` |

---

## 3. Security

| Control | Detail |
|---|---|
| CSRF | Double-submit cookie `paratrack_csrf` + `X-CSRF-Token` / `csrf_token` on every mutation |
| Auth rate limit | login 10/min per IP and email, with a 200/min IP ceiling; register 30/min, forgot 5/min, reset 10/min per IP; 429 + `Retry-After` |
| Headers | `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, CSP, HSTS on TLS |
| Passwords | bcrypt |
| API tokens | high-entropy `pt_`, SHA-256 at rest, raw shown exactly once |
| Password reset | 30 min single-use token, always answers `?sent=1` (no user enumeration) |
| Webhooks | `X-Paratrack-Signature: hex(hmac-sha256(secret, body))` |
| Sessions | HttpOnly + `Secure` cookies when the request is TLS |
| Client contacts and money | A member sees a client's name, its projects and the time on them. Email, requisites, internal notes, money and invoices reach managers only, on the pages and in `/api/v1/clients` (D-2, ADM-08) |

---

## 4. HTTP surface

### Public

```
GET  /login /register /forgot-password /reset-password
GET  /lang/{code}
GET  /sso/login /sso/callback          (OIDC, env-gated)
GET  /sw.js  /static/  /static/manifest.webmanifest
POST /api/login /api/register /api/logout
POST /api/password/forgot /api/password/reset
POST /api/stripe/webhook               (public, HMAC-verified)
```

### Authenticated pages

```
/ /stats /graph /goals /tags /timesheet /schedule
/payroll /payroll/{id}
/reports /reports/run
/invoices /invoices/{id} /invoices/{id}/pdf
/projects /projects/new /projects/{slug}
/tasks /tasks/{id}
/integrations /integrations/marketplace /integrations/{id}
/settings/team|members|invites|profile|tokens|webhooks|audit|notifications
/welcome /help
/invites/{token}
```

### Authenticated API

```
/api/sessions  (start, stop, pause, resume, PATCH, DELETE)
/api/sessions/backfill
/api/focus/{name}
/api/timesheet/cell
/api/timesheet/approve  /api/timesheet/reopen   (manager)
/api/team/modules   (sections choice)
/api/schedule/cell
/api/member/pay
/api/team/stripe
/api/projects /api/projects/{id}
/api/tasks  (list; `?archived=1` for the archive; create)  /api/tasks/{id}  (update)
/api/tasks/{id}/status  /api/tasks/{id}/archive  /api/tasks/{id}/restore
/api/workspace/restore  (raw JSON body of an export, creates a workspace for the caller)
/api/tasks/{id}/activities  /api/tasks/{id}/linkable
/api/goals /api/tags
/api/reports.csv  /api/reports/save  /api/reports/{id}/delete
/api/tokens  /api/tokens/{id}/delete
/api/webhooks  /api/webhooks/{id}/delete
/api/integrations  /api/integrations/{id}/sync|delete  /api/integrations/start
/api/me  /api/external-tasks
/api/push/key  /api/push/subscribe  /api/push/unsubscribe
/invoices  /invoices/{id}/status|delete|pay|paid|payments|letter.eml
/projects/{slug}/norm (weekly norm, manager)
/payroll  /payroll/{id}/paid|delete
```

### API v1 (Bearer `pt_…` or session cookie)

```
GET|POST   /api/v1/sessions
PATCH|DELETE /api/v1/sessions/{id}
GET        /api/v1/projects
GET        /api/v1/clients
GET        /api/v1/tags
GET        /api/v1/tasks
GET        /api/v1/reports/summary
```

`GET /api/v1/sessions` returns **both** closed and still-running sessions. Paged newest first: `?limit=` (default 100, max 500), and `next_cursor` from the response goes back as `?cursor=` for the next page.

---

## 5. Data model (Postgres)

Core tables: `users`, `teams`, `memberships`, `activities`, `sessions`, `projects`,
`tags`, `session_tags`, `goals`, `saved_reports`, `password_reset_tokens`, `tasks` (a task belongs to a project; `activities.task_id`
holds at most one task per activity).

Wave tables: `api_tokens`, `integrations`, `external_tasks`, `invoices`, `invoice_lines`,
`audit_log`, `webhooks`, `webhook_deliveries`, `payroll_runs`, `payroll_lines`, `timesheet_approvals`,
`schedule_entries`, `push_subscriptions`, `settings` (VAPID keys).

Rules worth knowing:

- All domain data is `team_id`-scoped
- `teams.owner_id` is `NOT NULL` → fixtures must insert users before teams
- Nullable TEXT/INTEGER scans need `sql.NullString` / `sql.NullInt64`
- Queries use `?` placeholders; `db.Conn` rebinds them to `$1, $2, …`
- `columnMigrations` runs before `uniqueMigrations`

---

## 6. Offline mode & push

**Offline** (`/static/js/app.js`)

- Service worker at `/sw.js` (root scope `/`) caches the app shell; pages are network-first
- Offline banner: `Offline — changes will sync when you reconnect`
- Serializable same-origin mutations (HTMX and `fetch`) are queued in `localStorage` with their body encoding
- On reconnect or page load the queue drains; only **2xx** dequeues. A failed item stays first in line and offers retry or explicit removal. Unsupported bodies and cross-origin URLs are not queued or replayed.

**Push** (`Settings → Notifications`)

- VAPID keys generated and stored in the DB
- `GET /api/push/key` → `{ publicKey }`
- `POST /api/push/subscribe` (`endpoint`, `p256dh`, `auth`) / `POST /api/push/unsubscribe`
- Events: session stopped, invoice paid, payroll paid, goal threshold

---

## 7. Client extension

`extension/` — MV3 **popup-only** (no content scripts, no `scripting`).
Popup timer + task picker. Config (base URL + `pt_` token) in `chrome.storage.local`.
`host_permissions` must list every origin reached.

---

## 8. Run

```bash
# dev
go run ./cmd/paratrack web --addr 127.0.0.1:8888

# production build (static, no CGO)
CGO_ENABLED=0 go build -ldflags='-s -w' -o paratrack ./cmd/paratrack

# UI assets
make ui
```

Env knobs: `PARATRACK_SMTP_*`, `PARATRACK_JIRA_SITE`, `PARATRACK_GITLAB_SITE`,
`PARATRACK_STRIPE_KEY`, `PARATRACK_STRIPE_WEBHOOK_SECRET`,
`PARATRACK_OIDC_ISSUER`, `PARATRACK_OIDC_CLIENT_ID`, `PARATRACK_OIDC_CLIENT_SECRET`,
`PARATRACK_PUBLIC_URL` (required HTTPS origin in production),
`PARATRACK_TRUSTED_PROXIES` (comma-separated CIDR ranges for reverse proxies;
forwarded host/proto/IP headers are ignored from all other peers). The public
HTTPS origin marks cookies and HSTS secure behind a TLS terminator; trusted
proxy ranges remain necessary before forwarded headers affect request data;
`PARATRACK_INTEGRATION_ALLOW_PRIVATE` (explicitly allow private Jira/GitLab
addresses; default is to allow public destinations only).

---

## 9. QA

CI runs static, architecture, dependency, race-enabled Go and browser E2E gates
for pushes and pull requests targeting `main` or `master`. The expanded product,
business-logic and offline acceptance suites are manual; their last recorded
results are in [QA.md](./QA.md) and are not a claim about the current working
tree.

```bash
go test ./...                                   # unit + integration
. .venv/bin/activate
python e2e/qa_full.py                           # UI / visual (Playwright + screenshots)
python e2e/qa_logic.py                          # business-logic math (HTTP)
python e2e/qa_offline_push.py                   # offline + push
```

---

## 10. Known limitations

- Stripe Checkout and OIDC are wired but untested against real providers
- Live provider import needs real PATs (no sandbox credentials in-repo)
- `NextInvoiceNumber` returns an advisory display value; concurrent reads may return the same value, while invoice draft creation allocates under a workspace lock. Payroll numbers are allocated inside atomic payroll draft creation.
- Rate limiting is per-process (single-binary target), with at most 10,000 tracked keys per limiter; when full, unseen keys fail closed until cleanup frees capacity
- No email verification on signup
- Approval is per workspace week: one approval covers every member's time in that week. There is no per-member approval.
- A pay run cannot cover the current week, because the week must end and be approved first.
- Pay runs are hourly only; there is no per-piece or salary scheme.
