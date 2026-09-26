# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

Installable PWA with a native-feeling phone shell (bottom tab bar, "More" sheet, running-timer bar) below 1024 px and a desktop layout above it. No native apps.

## Users

Three audiences, weighted equally (confirmed 2026-09-26):

- **Someone tracking their own time.** Wants to see where the day went; invoices and teams are secondary. Opens the app many times a day to start, pause and stop timers, often from a phone.
- **A freelancer with several clients.** Tracks per client project, sets hourly rates, turns billable time into invoices and marks them paid.
- **A small team (studio or agency, roughly 3–20 people).** Shares a workspace, plans people across projects on a weekly schedule, pays members from tracked time.

The product must carry one person from solo tracking to a small team without switching tools.

## Product Purpose

paratrack is a strict, honest ledger of where time goes: live timers, a weekly timesheet, statistics, and the money built on that time (client invoices, member payouts). Success is that a user trusts the numbers enough to bill and pay from them, and never has to reconcile in a spreadsheet.

## Positioning

Against Toggl, Clockify and Harvest, all four of these hold together (confirmed):

- **Your own server if you want it.** Runs as a public service at https://paratrack.duckdns.org, and the same product self-hosts as one docker compose stack (app + Postgres) or a single binary on SQLite. Data can stay with the user.
- **Parallel timers.** Several activities run at once; "Only this" pauses the rest. Most trackers allow one.
- **Everything in one place.** Tracking, invoices, payouts, schedule and reports are built in, not upsells or add-on integrations.
- **Russian first.** The interface is Russian by default with English second; copy is written for Russian speakers, not translated from English.

## Operating Context

- Timers are started and stopped throughout the day, frequently on a phone as an installed PWA; offline actions queue and replay with their original click time.
- Weekly rituals: filling gaps in the timesheet, reviewing statistics, generating invoices for a period, running payroll.
- Money is exact: integer cents, billable time rounded to 0.01 h (36 s) and priced from the rounded hours, so "hours × rate = amount" holds on every document. Invoices and pay runs are numbered `INV-YYYY-NNN` and `PAY-YYYY-NNN`; invoices export to PDF.
- Integrations import tasks from GitHub, GitLab, Jira, Trello, Asana, ClickUp, Todoist and Notion; history imports from Toggl, Harvest and Clockify. REST API with `pt_` tokens, webhooks, audit log, optional OIDC SSO, browser extension.

## Capabilities and Constraints

- Web UI is server-rendered Go (html/template) with HTMX and Alpine; Tailwind v4 + DaisyUI v5. Postgres in the compose deployment, SQLite for the CLI and single-binary installs.
- A CLI (`paratrack start|stop|stats…`) works against local SQLite and is a real feature, not a dev tool.
- Terminology in the Russian UI: Обзор, Статистика, Табель, Проекты, По часам, Цели, Теги, Счета, Выплаты, Расписание, Отчёты; «Идут сейчас» for running timers, «Только эта» for focus. English: Dashboard, Stats, Timesheet, Projects, By hour, Goals, Tags, Invoices, Payroll, Schedule, Reports, "Running now", "Only this".
- Open decisions: pricing and any paid tier are undecided; do not state that the service is free or paid.

## Brand Commitments

- Name is lowercase **paratrack**.
- Voice is plain and direct: every screen says what it is for in one line, fields carry labels and hints, no jargon or internal codes in the UI (users asked for self-documenting screens, 2026-09-26).

## Evidence on Hand

- Real product screenshots in `e2e/screenshots/readme/` and QA captures in `e2e/screenshots/qa/`.
- Product and business-logic reference in `docs/PRODUCT.md`, journey map in `docs/JOURNEY.md`.
- License: MIT (`LICENSE`).
- No testimonials, customer logos, usage numbers, press or benchmarks exist; do not invent any.

## Product Principles

1. **The numbers must be trustworthy.** Durations, totals and money agree across every screen and document; correctness beats cleverness.
2. **Starting a timer is the first thing, everywhere.** It is reachable in one step on every device, and what is running is always visible.
3. **Grow from one person to a small team without changing tools.** Team, billing and payroll features stay out of the way of someone tracking alone.
4. **Every screen explains itself.** Title, one-line purpose, labeled fields, a next step in every empty state.
5. **The user's data is theirs.** Self-hosting stays a first-class path, not an afterthought.
