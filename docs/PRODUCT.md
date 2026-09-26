# paratrack

Minimalist time tracker with parallel activities, team workspaces and billing.
Single Go binary, embedded web UI, SQLite. No Node at runtime, no CGO.

**Live:** https://paratrack.duckdns.org

---

## 1. What it does

| Area | Capability |
|---|---|
| **Timer** | Start / pause / resume / stop parallel activities, inline edit of start, end and duration |
| **Backfill** | Add a past session in natural language (`yesterday 09:00` → `11:30`) |
| **Timesheet** | Week grid (activity × days), cell is the source of truth |
| **Projects** | Colored projects, estimates vs actual, billable rate |
| **Stats / Graph** | Breakdown by project and activity, distribution, ECharts graph, CSV export |
| **Goals** | Daily / weekly / monthly minute targets per activity |
| **Tags** | Free-form labels on sessions, filterable |
| **Invoices** | `INV-YYYY-NNN`, draft → sent → paid, PDF, online payment link |
| **Payroll** | `PAY-YYYY-NNN` pay runs from tracked time × member rate |
| **Schedule** | People × week planning grid, load % against capacity |
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

**Payroll**

- Member carries `hourly_pay_cents` (0 = not on payroll) and `capacity_minutes` (default 480)
- Runs skip members with pay 0
- Sessions attribute via `sessions.user_id` (stamped on timer start)
- Legacy rows without a user fall on the team's first paid member

**Schedule**

- `schedule_entries` per (team, user, project, day), cell minutes, 0 clears
- `Load% = planned / (capacity × 7)`

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
| Auth rate limit | login 10/min, register 5/min, forgot 5/min, reset 10/min per IP; 429 + `Retry-After` |
| Headers | `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, CSP, HSTS on TLS |
| Passwords | bcrypt |
| API tokens | high-entropy `pt_`, SHA-256 at rest, raw shown exactly once |
| Password reset | 30 min single-use token, always answers `?sent=1` (no user enumeration) |
| Webhooks | `X-Paratrack-Signature: hex(hmac-sha256(secret, body))` |
| Sessions | HttpOnly + `Secure` cookies when the request is TLS |

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
/integrations /integrations/marketplace /integrations/{id}
/settings/team|members|invites|profile|tokens|webhooks|audit|notifications
/invites/{token}
```

### Authenticated API

```
/api/sessions  (start, stop, pause, resume, PATCH, DELETE)
/api/sessions/backfill
/api/focus/{name}
/api/timesheet/cell
/api/schedule/cell
/api/member/pay
/api/team/stripe
/api/projects /api/projects/{id}
/api/goals /api/tags
/api/reports.csv  /api/reports/save  /api/reports/{id}/delete
/api/tokens  /api/tokens/{id}/delete
/api/webhooks  /api/webhooks/{id}/delete
/api/integrations  /api/integrations/{id}/sync|delete  /api/integrations/start
/api/me  /api/external-tasks
/api/push/key  /api/push/subscribe  /api/push/unsubscribe
/invoices  /invoices/{id}/status|delete|pay|paid
/payroll  /payroll/{id}/paid|delete
```

### API v1 (Bearer `pt_…` or session cookie)

```
GET|POST   /api/v1/sessions
PATCH|DELETE /api/v1/sessions/{id}
GET        /api/v1/projects
GET        /api/v1/reports/summary
```

`GET /api/v1/sessions` returns **both** closed and still-running sessions.

---

## 5. Data model (SQLite)

Core tables: `users`, `teams`, `memberships`, `activities`, `sessions`, `projects`,
`tags`, `session_tags`, `goals`, `saved_reports`, `password_reset_tokens`.

Wave tables: `api_tokens`, `integrations`, `external_tasks`, `invoices`, `invoice_lines`,
`audit_log`, `webhooks`, `webhook_deliveries`, `payroll_runs`, `payroll_lines`,
`schedule_entries`, `push_subscriptions`, `settings` (VAPID keys).

Rules worth knowing:

- All domain data is `team_id`-scoped
- `teams.owner_id` is `NOT NULL` → fixtures must insert users before teams
- Nullable TEXT/INTEGER scans need `sql.NullString` / `sql.NullInt64`
- Never issue a second query while a `rows` cursor is open (SQLite deadlock)
- `columnMigrations` runs before `uniqueMigrations`

---

## 6. Offline mode & push

**Offline** (`/static/js/app.js`)

- Service worker at `/sw.js` (root scope `/`) caches the app shell; pages are network-first
- Offline banner: `Offline — changes will sync when you reconnect`
- Mutating requests (HTMX and `fetch`) are queued in `localStorage` **with their body**
- On reconnect the queue drains; only **2xx** dequeues — a 4xx/5xx keeps the item

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
`PARATRACK_OIDC_ISSUER`, `PARATRACK_OIDC_CLIENT_ID`, `PARATRACK_OIDC_CLIENT_SECRET`.

---

## 9. QA

Four suites, all green on the current build. See [QA.md](./QA.md) for the matrix.

```bash
go test ./...                                   # unit + integration
. .venv/bin/activate
python e2e/qa_full.py                           # UI / visual (Playwright + screenshots)
python e2e/qa_logic.py                          # business-logic math (HTTP)
python e2e/wave9_verify.py                      # offline + push
```

---

## 10. Known limitations

- Stripe Checkout and OIDC are wired but untested against real providers
- Live provider import needs real PATs (no sandbox credentials in-repo)
- `NextInvoiceNumber` / `NextPayrollNumber` race under concurrent creation
- Rate limiting is per-process (single-binary target)
- No email verification on signup
- `GetActivity` is not team-scoped at the helper level
- BuildReport does a per-row project lookup (N+1)
