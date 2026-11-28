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
  `offline` when it has nothing, `?` alone when its last answer is older than
  15 minutes, `auth?` when the token is refused, `access?` when it no longer has a
  seat in the workspace, and never exits with an error. The cached line
  belongs to one server and token and is not shown for another. Locally,
  without a server, `--short` keeps the same contract.
- A report can be narrowed to one project or to one client card's projects;
  the scope is printed under its title and carried into the CSV, and the
  plan column is left out of a scoped report. An id that is not the
  workspace's own is ignored.
- The accountant's export downloads either the invoice lines or the ledger
  of received payments (`/invoices/payments.csv`: date paid, invoice, client,
  the client's ИНН from the invoice's details, amount, source) for a period
  and optionally one client, plain or in the 1С/Excel dialect.
- An emailed invoice carries its act and the breakdown by session (CSV)
  besides the invoice PDF (the act can be left out for a client who signs it
  after paying). With the executors' names hidden on invoices, the
  breakdown leaves the person column empty too. Whether the names are hidden
  is frozen on the invoice when it is sent or emailed (`invoices.hide_names`):
  a draft follows the workspace switch, a sent document reads as it went out.
- An accountant role reads the money without managing: invoices, payouts and
  reports with their exports, recording payments and closing a month, never
  editing a document, a rate or the time. A month an accountant closed is
  reopened only by them or the owner.
- A manager repeats the week on screen in up to 12 weeks ahead; a week that
  already has a plan is skipped and named, never merged.
- A sent invoice reads as it went out: the seller, the logo, the line labels,
  the issue day (the day it was sent, not the day the draft was made) and
  the session breakdown are kept at send (`invoices.issued_snapshot`); the
  payment state, what is still owed and the QR stay live. A client link is
  made only for a sent invoice.
- A sent invoice with nothing paid is voided with a reason, never deleted:
  its number stays used, its hours can be billed again, payments are refused,
  the PDF says АННУЛИРОВАН and its client link answers that it was voided.
- A payment entered by mistake is reversed with a reason: a negative entry in
  the journal, never a deletion; a paid invoice goes back to sent. A payment
  cannot be dated in the future or into a closed month.
- A pay run is marked paid on a payout day (`payroll_runs.paid_on`), refused
  in a closed month; the accountant may do it.
- A closed month keeps the closer's role (`closed_periods.closed_by_role`), so
  removing an accountant does not let an admin reopen their month. An import
  is refused whole when an entry starts in a closed month.
- Every CSV has an Excel dialect for a person: headers in the reader's
  language, ';', a decimal comma, ДД.ММ.ГГГГ and an ИНН kept as text. The
  session breakdown that goes to the client is always in it. Reports also
  export for Excel and for the client (no rates), and name their scope on
  every row. The accountant's exports follow the client card, not the spelling.
- A limit goal warns while the time is being spent: at 80% and when it is
  crossed, once each per period, to whoever is timing the activity, not
  after the stop. The forgotten-timer reminder is not held for the morning.
  On an iPhone the notifications screen says to add the app to the Home
  Screen. A personal space without invoices or CRM shows no Clients entry.
- The git hook finds paratrack on PATH in the printed form for hook managers;
  a ticket earlier in the branch leads the activity name (PT-123/fix-login →
  PT-123-fix-login, the pattern set by git config paratrack.ticketPattern);
  paratrack.pauseBranches adds branches that pause, as does a detached HEAD
  or a branch with nothing usable in its name; a numeric branch is an
  activity, never a session id. Completion asks the server for the caller's
  own sessions (`/api/v1/sessions?mine=1`), keeps the list a minute, loads
  compinit in zsh when missing and completes fish only right after the verb.
- Budget warnings price the time the way the project screen and the invoice
  do (a session moved to another project counts there), add a forecast level
  when tracked plus what the schedule still plans would go over, come again
  for new figures after a raise, and wait until no recipient is asleep.
  Overload reminders look at this week and the next, and a plan on someone
  with no available time is an overload.
- The schedule shows each person's load over four weeks from the one on
  screen. A repeated plan skips holidays, absences and the people who already
  have a plan in that week, instead of skipping the week for everyone.
- Each member has work days (`memberships.work_days`, ISO digits, Monday to
  Friday by default): the norm, the schedule's capacity, the team week and
  the day-fill reminder follow them; a worked Saturday reminds too. The
  timesheet fills the Russian production calendar's weekdays off for 2026 in
  one click, each named.
- The issue day is one column (`invoices.issued_on`): the document, its QR
  (always ДД.ММ.ГГГГ, whatever the screen's language), and the accountant's
  register (date and period filter) read it; the register also uses the kept
  line labels and seller. Invoices issued before it was kept took their
  creation day once. The letter file (.eml) issues the invoice like the send:
  the look is kept, and the letter carries the act and the session breakdown;
  marking the invoice sent later keeps that day unless the draft changed.
- Running timers are watched every minute (the forgotten timer, limit goals);
  the rest of the reminders keep their hourly pass. Limits have their own
  notification topic and are "crossed" at 100%. The budget forecast counts
  the plan from tomorrow in the workspace's calendar; budget warnings never
  step back to a lower level for the same figures, go out by mail too, and
  overload and budget notices both wait until no recipient is asleep.
- A workspace file carries each member's work days. The production calendar
  never overwrites a day the manager marked, and fills a year in one
  transaction; the timesheet offers each year the server has a calendar for.
  Payroll and task reports have an Excel download; the accountant's export
  starts with Excel ticked.
- A manager can revoke an invoice's client links: every link made for it
  stops opening at once and answers 404, the same as an expired one.
- A goal is a target ("at least") or a limit ("at most", `goals.kind =
  'max'`): crossing a limit sends "Лимит превышен", never a congratulation,
  and the goal shows red.
- A project with an estimate (hours) or a money budget (counted at its rate)
  warns its managers and lead at 80% and again at 100%, each once
  ("project.budget", mutable).
- A timer running longer than its owner set (4 hours unless they chose
  otherwise in the preferences, 0 turns it off) gets one push per session
  ("timer.long", mutable like the others); at night it waits for the
  person's morning.
- `paratrack hook install` writes a git post-checkout hook that focuses the
  timer on the checked-out branch's last name part (letters of any alphabet,
  digits, dot, dash and underscore kept, anything else a dash; the name is
  made by `focus --branch` in Go), in the
  background, after a one-second settle so quick successive checkouts end on
  the branch still checked out; checking out main/master/develop/trunk
  pauses the timers, since review there is not the feature's time. It never
  touches a hook it did not write and refuses when core.hooksPath belongs to
  a hook manager; `paratrack hook print` gives the script to paste there. `paratrack completion bash|zsh|fish` completes commands
  and, after start and focus, the caller's own recent activities (400 ms at
  most); names are offered as quoted text, never evaluated. "Only this" (and the
  remote `focus`) starts an activity the workspace has not seen yet. It is a real
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
  the norm, empty weekdays, and approval per person, with "approve everyone
  on track" (80% of the norm, no empty weekday by the calendar) for a
  finished week; a date inside a week means that whole week. A manager
  records a member's vacation, sick leave or day off over a span of days
  and the workspace's holidays and transferred working days (a Saturday
  marked as worked counts in the norm, the schedule and the team week)
  ("Absences and holidays" on the timesheet);
  those days leave the norm, and a member away the whole week is "away",
  which the button approves too. Absences live in their own table
  (member_absences), apart from the member's own day marks, which can no
  longer overwrite or clear them; the timesheet lists the spans with a way to
  remove them. Nobody away, and nobody on a workspace holiday, gets the
  evening "fill your day" reminder, and the team week on Stats names those
  days instead of flagging them. The schedule counts a week's capacity on
  its working days only (a holiday or an absence takes the day out, admin
  time shrinks with it), names the absences on the person's row, and the
  overload reminder reads the same numbers. Approved
  and on track have different marks.
- A pay run cannot cover the current week, because the week has to end and be
  approved first. A pay run can cover a chosen set of members, or only the
  hourly or only the salaried ones, and the overlap check compares only the
  same members. Draft pay runs can include one-off bonus or deduction
  adjustments per member, as an amount or as a percent of what the member
  earned in the run. "Recalculate" prices a draft again from the current
  timesheet for the people on it (kept with the run, so someone whose hours
  were all taken off comes back when they return); fixed adjustments stay,
  percent ones are taken of the new earnings. It opens as a preview first:
  per person, what they get now and what they would get, who would be left
  with no earnings, and who the period pays but the run is not for. Nothing is
  written until the manager confirms, and a confirmation is refused if the
  amount of anyone on the run moved since the preview (money moving between
  two people with the total unchanged counts). A recalculation without a
  confirmed preview is refused. The audit log keeps both totals and every
  person whose amount or hours moved, with their id. A run lists people by
  name, each person's adjustments indented under their own line; an
  adjustment of someone with no hours or salary stands on its own row. A draft invoice takes manual one-off lines and discounts
  without touching the sessions; a discount cannot take the total below
  zero, and an invoice with a negative total cannot be sent. The labelled
  numbers in Russian details (ИНН, КПП, ОГРН, БИК, р/с, к/с) are checked by
  length and control digits when the workspace saves them and again, for the
  seller and the client, before an invoice reaches the client in any way:
  sent, emailed, paid straight from a draft, given a payment link or a client
  link. A draft carries the workspace's current details: saving them in the
  settings passes them to every unsent invoice; a sent one keeps its copy.
  A rouble invoice that still owes money carries a bank-app payment QR code
  in its PDF (ГОСТ Р 56042, ST00012) for the amount still owed, when the
  seller's details name an account, a БИК and a bank that check out
  (rsc.io/qr draws it); a paid invoice carries none. The purpose names the
  invoice date and how VAT stands. A payment dated into a closed month is
  refused.
  The "ИНН/КПП 7707083893/770701001" pair is read as 1C prints it, also
  with no КПП for a sole trader ("…/" or "…/ -"). Client numbers that do not
  check out are refused wherever they are written: the client card, a new
  invoice and a draft edit. A draft edit updates the client card only when
  "Update the client card too" is ticked, and an empty field never clears the
  card; on a project without a card, or an invoice over every project, the
  edit is refused with that reason, and the box is offered only when a card
  exists. Details that do not check out never travel to the card or to the
  project's copy the next invoice falls back to. Details are stored with LF
  line endings. A new invoice remembers the client's email and details on the
  client card only when "Update the client card too" is ticked and the
  invoice names that card's client (another name is refused), never clearing
  a filled field or renaming the card. A project without a card gets the
  card of the client its invoice names (found by name or made, only empty
  fields filled). The card is the one place a project's client is kept: a
  draft edit no longer writes a copy onto the project, and the copies left
  from before were turned into cards (the columns stay, unwritten). A
  card or a draft saved before the check can still be edited while its
  details stay as they are.
- A token whose owner has no seat in its workspace any more is answered
  403, an unknown or revoked one 401. The browser extension shows the
  server's reason for either on every action, in the section on screen, a
  server error page as a server error, an address that does not answer as
  paratrack as such, and "server unreachable" only when nothing answered. A
  token is saved only after the server accepts it; a saved token that stops
  working brings the setup back with the reason, while a network blip keeps
  the timer screen. The settings show the saved address and a way back. The popup is Russian first
  (`_locales`). Its tests run the popup against its own popup.html
  (`node --test extension/`, part of `make test`).
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
