# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

An installable PWA with a phone shell below 1024 px: a 64 px top app bar, a
fixed bottom navigation with four destinations and a "More" sheet, Material's
48 dp touch floor, 16 px fields and a floating action for the timer. Above that
width the layout is a desktop one with a drawer. There are no native apps.

The server is one Go binary without CGO, and Postgres 17 is the only store. The
same binary also serves a CLI, and the public site and the user documentation
are a separate static bundle in `site/`.

## Users

Three audiences, weighted equally (confirmed 2026-09-26):

- **Someone tracking their own time.** Wants to see where the day went;
  invoices and teams are secondary. Opens the app many times a day to start,
  pause and stop timers, often from a phone.
- **A freelancer with several clients.** Tracks a project per client, sets
  hourly rates, turns billable time into invoices and marks them paid.
- **A small team, a studio or an agency of roughly 3 to 20 people.** Shares a
  workspace, plans people across projects on a weekly schedule, and pays
  members from tracked time.

The product has to carry one person from tracking alone to a small team
without a change of tool. The interview notes in
[docs/research/studio-interviews.md](docs/research/studio-interviews.md) are a
draft for the first round; they set out who to talk to and which questions are
not being asked, and no decision is taken from them yet.

## Product Purpose

paratrack is a strict, honest ledger of where time goes: live timers, a weekly
timesheet, statistics, and the money built on that time, which means client
invoices and member payouts.

Success for the user is trusting the numbers enough to bill and pay from them,
and never reconciling in a spreadsheet. Success for the product is that the
same tracked minutes produce the same amount on the screen, in the PDF and in
the payout.

## Positioning

Against Toggl, Clockify and Harvest, four things hold together (confirmed):

- **Your own server, if you want it.** The product runs as a public service at
  https://paratrack.duckdns.org, and the same code self-hosts as one docker
  compose stack, the app beside a Postgres, or as a single binary next to any
  Postgres. The data can stay with the person who tracked it.
- **Parallel timers.** Several activities run at once, and "Only this" pauses
  the rest. Most trackers allow one.
- **The whole job in one place.** Tracking, invoices, payouts, the schedule and
  the reports are built in, rather than sold as upsells or left to add-on
  integrations.
- **Russian first.** The interface is Russian by default and English second.
  The copy is written for Russian speakers rather than translated from English.

## Operating Context

- Timers are started and stopped throughout the day, frequently on a phone as
  an installed PWA. Offline actions queue in the browser and replay with the
  time of the click.
- The weekly rituals: filling gaps in the timesheet, reviewing statistics,
  generating an invoice for a period, running payroll.
- Money is exact. Amounts are integer cents, billable time is rounded to 0.01 h
  (36 s) and priced from the rounded hours, so hours × rate = amount holds on
  every document. Invoices and pay runs are numbered `INV-YYYY-NNN` and
  `PAY-YYYY-NNN`, and an invoice exports to PDF.
- Integrations import tasks from GitHub, GitLab, Jira, Trello, Asana, ClickUp,
  Todoist and Notion; history imports from Toggl, Harvest and Clockify. A REST
  API with `pt_` tokens, signed webhooks, an audit log, optional OIDC single
  sign-on and a browser extension sit around them.
- A manager approves a week of the timesheet. While a window is approved, any
  write whose session starts inside it is refused, so the numbers a payout is
  computed from cannot change underneath it. The refusal names the week and says
  that the owner or an admin can reopen it.

## Capabilities and Constraints

Confirmed by the code:

- All screens and the shared navigation shell are React 19 on shadcn/ui and
  Tailwind v4, and Go renders the `base.html` shell with typed bootstrap data
  for them (`email.html` formats outgoing emails).
- Postgres is the only database, and the schema is created and migrated at
  startup.
- A CLI (`paratrack start|stop|stats...`) works against the same Postgres
  through `PARATRACK_DATABASE_URL`, or remotely over HTTP with `PARATRACK_URL`
  and a bearer `PARATRACK_TOKEN` (`pt_...`). `paratrack auth login` stores the
  address and the token in `~/.config/paratrack/config.json` (mode 0600), a
  `.paratrack` file in the project directory overrides it, and the environment
  overrides both. A command the remote mode does not support fails with the list
  of the ones it does, and never falls back to a local Postgres.
  `status --short` is meant for a shell prompt: it answers within 0.4 s, draws
  the line from its last answer (marked `?`) when the server is slow, prints
  `offline` when it has nothing, and never exits with an error. It is a real
  feature rather than a development tool. `stats` counts a running timer the way the web stats do,
  so the CLI and the screen agree on today.
- An agent reaches the workspace through `POST /mcp` with an API token and the token owner's rights. Deleting an
  invoice or a pay run, reopening an approved week and deleting the workspace are only requested. A person confirms
  each request in the app, under Settings, Confirmations.
- A workspace picks its sections. The core ledger, the dashboard, statistics,
  the timesheet and the projects are always there; twelve more can be switched on
  at registration or later. Four presets ship: "For myself", "Freelancer",
  "Studio: start" (payroll and schedule without CRM, tasks, goals and calendar)
  and "Studio". Each preset names the sections it leaves off; "For myself" adds a hint to pick
  "Studio or team" when the workspace has a team (ADM-16).
- The Russian terminology of the interface: Обзор, Статистика, Табель, Проекты,
  По часам, Цели, Теги, Счета, Выплаты, Расписание, Отчёты, and «Идут сейчас»
  for running timers, «Только эта» for focus. In English: Dashboard, Stats,
  Timesheet, Projects, By hour, Goals, Tags, Invoices, Payroll, Schedule,
  Reports, "Running now", "Only this".
- The clients of a workspace have contacts, deals in the workspace's own stages (five by default, a manager
  can change them) and activities (calls, meetings, notes), and the workspace shares one calendar of events; a member can also sync their own Google or Outlook calendar into it (CRM-19). Both are sections (`crm`, `calendar`). Clients carry tags that a manager sets, and a search finds clients and contacts by name, tag, phone and, for managers, billing email (CRM-18).
  A won deal makes its project in one step, with its client and budget; a manager sets the rate in the project. Only a manager sees the budget and the spend on a project.
  Deal amounts are shown and set by managers only; a member sees the deal without its amount.
- An agent works with the same entities through `POST /mcp` with a bearer `pt_` token. A read-only token
  reads only, and every write is recorded in the audit log.
- Every page and every `/api/*` call passes through `requireAuth`, with a
  session cookie or a bearer `pt_` token. Reads are scoped to the workspace,
  and for a member to their own sessions.
- The database stores the session token's SHA-256 digest, never the token, so
  a leaked dump cannot be replayed as logins. A password change keeps the
  browser that asked for it and signs every other device out.
- The account can require a second factor: a TOTP code from an authenticator
  app and ten one-time backup codes. Turning it off needs the password and a
  live code. API tokens are unaffected.
- A single sign-in never enters an account whose address nobody has
  confirmed. It goes to the password-reset flow instead, which proves the
  address and clears the credentials whoever set them: a registration under
  someone else's address cannot capture the owner's SSO.
- Money is integer cents throughout, and the arithmetic lives in
  `internal/money`.

Deliberately not true, and worth knowing before a change:

- Stripe Checkout is wired but has never been run against a real provider.
  OIDC has: the full SSO cycle was run against a live Dex in November 2026.
- Live provider import needs real personal access tokens. There are no
  sandbox credentials in the repository.
- Email verification on signup is on by default; an operator can switch it
  off with `PARATRACK_REQUIRE_EMAIL_VERIFICATION=0`, which is how the browser
  suite and self-hosts without SMTP run it. The confirmation link lives 48
  hours, and password reset confirms the address too.
- Approval works per workspace week or per member within a week. The
  timesheet shows a manager every member's week on one screen: hours against
  the norm, empty weekdays, and approval per person.
- A pay run cannot cover the current week, because the week has to end and be
  approved first. A pay run can cover a chosen set of members, or only the
  hourly or only the salaried ones, and the overlap check compares only the
  same members. Draft pay runs can include one-off bonus or deduction
  adjustments per member, as an amount or as a percent of what the member
  earned in the run. "Recalculate" prices a draft again from the current
  timesheet for the people on it; fixed adjustments stay, percent ones are
  taken of the new earnings. A draft invoice takes manual one-off lines and discounts
  without touching the sessions; a discount cannot take the total below
  zero, and an invoice with a negative total cannot be sent. The labelled
  numbers in Russian details (ИНН, КПП, ОГРН, БИК, р/с, к/с) are checked by
  length and control digits when the workspace saves them and again, for the
  seller and the client, before an invoice is sent or emailed. Rebuilding a
  draft takes a fresh copy of the workspace's details.
- Pay runs are hourly or a fixed monthly salary per calendar month the run touches. There is no per-piece scheme.
- A pay run downloads as CSV, one row per person or, by day, one row per person per day with the week it falls in. The amounts stay on the person rows.
- Rate limiting is per process, with at most 10,000 tracked keys per limiter.
- Open decision: pricing, and whether there is a paid tier at all. Do not state
  that the service is free or that it is paid.

## Brand Commitments

- The name is lowercase **paratrack**.
- The voice is plain and direct. Every screen says what it is for in one line,
  fields carry labels and hints, and no internal codes appear in the interface.
  Users asked for self-documenting screens (2026-09-26).
- The image is a strict, honest ledger on a warm sheet of paper, and it has to
  survive a redesign. The tokens live in [DESIGN.md](DESIGN.md).

## Evidence on Hand

- The product screenshots in `e2e/screenshots/readme/`, the QA captures in
  `e2e/screenshots/qa/`, and the published shots in `site/assets/shots/`.
- The product and business-logic reference in
  [docs/PRODUCT.md](docs/PRODUCT.md), the journey map in
  [docs/JOURNEY.md](docs/JOURNEY.md), and the check matrix in
  [docs/QA.md](docs/QA.md).
- The licence is MIT, in [LICENSE](LICENSE).
- Absent, and not to be invented: no testimonials, no customer logos, no usage
  numbers, no press pieces and no benchmarks. The interview notes are a draft
  for the first round, not results.

## Product Principles

1. **The numbers have to be trustworthy.** Durations, totals and money agree
   across every screen and every document. Correctness beats cleverness.
2. **Starting a timer is the first thing, everywhere.** It is one step away on
   every device, and what is running is always visible.
3. **Grow from one person to a small team without changing tools.** Team,
   billing and payroll stay out of the way of someone tracking alone.
4. **Every screen explains itself.** A title, a one-line purpose, labelled
   fields and a next step in every empty state.
5. **The user's data is theirs.** Self-hosting stays a first-class path rather
   than an afterthought.

## Accessibility & Inclusion

What the code holds to:

- The phone shell follows Material's 48 dp touch floor and keeps the fields at
  16 px, so iOS does not zoom when a field takes focus. Both live in
  `web/src/ui.css`.
- The date and time fields read the document language rather than the browser
  locale, so the order on screen is the order the reader expects, and a value
  that could not be read submits nothing instead of the previous value.
- `prefers-reduced-motion` is honoured in the stylesheet: the skeleton
  animation stops, and the themed transition timing is applied only when the
  reader has no preference.
- `e2e/a11y_check.py` measures the live DOM on ten routes in both themes. It
  checks WCAG AA contrast for every visible text node, the 3:1 boundary of
  every form control, and that a control's accessible name contains its
  visible text, so a voice-control user can say what they see. It runs in CI.

What is not decided:

- The project has no written accessibility standard, and nobody outside the
  repository has audited the screens. The measurements above come from the
  automated detector alone.
- No accessibility needs have been collected from the users. The research in
  `docs/research/studio-interviews.md` covers the work, not access, and it has
  not been carried out.
- The detector has a known blind spot: the shadcn `Card` paints with a ring
  rather than a border, so a check that looks for `border-width` finds no cards
  and reports a clean sweep it did not earn.
