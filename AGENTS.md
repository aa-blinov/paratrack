# AGENTS.md — agent context for `paratrack`

A time tracker whose tracked minutes turn into client invoices and team payouts.
One Go binary serves the web app and the CLI, and Postgres 17 is the only store.
The screens are React 19 on shadcn/ui with Go templates and HTMX fragments
underneath; the styles and the bundle are embedded in the binary.

This file is what you need to work on the project without reading all of it.

## Run / build / test

```bash
make ui            # npm ci, the Tailwind CSS build and the React bundle
make build         # ./paratrack, and `make ui` runs first
make test          # scripts/test.sh: go test on a throwaway Postgres in docker
make vet           # go vet
make verify        # tidy -diff, vet, govulncheck, build, the architecture check
make architecture  # scripts/check-architecture.sh
make web           # build and serve the embedded UI on 127.0.0.1:8888
make e2e-up        # start that server in the background
make e2e           # the Playwright suite against it
make qa            # throwaway database, server, suite and teardown, one command
```

Go 1.27.2, Node 22 and Postgres 17. The browser suite needs a Python
virtualenv, which `bash scripts/setup_e2e.sh` creates. `scripts/test.sh` takes
the arguments `go test` takes, so `scripts/test.sh -run TestName ./internal/web`
runs one test.

`make lint` runs staticcheck. It is not part of `make verify`, and the reason
is in the Makefile.

## Where things live

| Path | What it holds |
| --- | --- |
| `cmd/paratrack/` | The process root: the environment is read here, the graph is built here, and the lifecycle is closed here. |
| `internal/web/` | The HTTP adapter. Routes, middleware, view models, templates, the JSON API. |
| `internal/cli/` | The CLI adapter. Command parsing and terminal output; it never imports the HTTP adapter. |
| `internal/appmodel/` | Application commands, results and edit fields that both transports use. |
| `internal/<feature>/` | The feature workflows: `auth`, `teams`, `tracking`, `invoicing`, `payroll`, `scheduling`, `reporting`, `webhooks`, `push`. |
| `internal/db/` | Postgres. The schema, the migrations, every query, and the authorization rules that live in the transaction. |
| `internal/model/` | Domain types and domain errors. No transport, no dependencies on other internal packages. |
| `internal/app/` | The service graph, shared by the CLI and the HTTP runner. |
| `web/src/` | The React screens, the shadcn primitives and the Tailwind entry point. |
| `web/src/offline/` | The offline queue, the replay policy and the banner. |
| `web/vite.config.ts` | Writes the bundle to `internal/web/static/ui/`, which the binary embeds. |
| `internal/web/static/ui/` | The built React bundle and the stylesheet. Both are tracked in git. |
| `internal/web/static/js/` | The bootstrap that runs before the page paints, the Sentry loader, and the slim ECharts build the graph page loads on demand. |
| `extension/` | The Chrome extension (MV3), a popup that talks to `/api` with a bearer token. |
| `e2e/` | The Playwright suite and the QA scripts. |
| `scripts/architecture/` | The rules the architecture check enforces, one file per area. |
| `site/` | The landing page and the user documentation, published on GitHub Pages. |
| `docs/` | ARCHITECTURE, PRODUCT, QA, JOURNEY, and the per-screen notes. |

## Traps

- **The committed bundle is what ships.** The built React bundle lives in
  `internal/web/static/ui/` and is tracked in git, and
  `internal/web/static_assets.go` embeds it. `go build` embeds whatever is
  there, so a change under `web/src/` does not reach the binary until
  `make ui` regenerates the bundle, and the regenerated files belong in the
  same commit. CI only asserts that the build produced a non-empty `app.js`
  and `app.css`, so a stale committed bundle passes the gate and ships.
- **The middot in an invoice line is data.** `internal/db/invoice_lines.go`
  builds the label as `project + " · " + activity`, and appends
  `" · " + person` for a per-person split. `OverlappingInvoices` compares those
  labels to catch the same hours on two documents, and `stampLegacyInvoices`
  matches on `p.name || ' · ' || a.name`. Changing the separator is a data
  migration over issued invoices, not a UI cleanup.
- **The write rule lives in the database, not in the route.** `POST
  /api/schedule/cell` sits behind a signed-in member and nothing more, because
  `UpsertScheduleEntry` checks the project lead and the membership in the same
  transaction. A route guard is not the authorization, and adding one does not
  replace the check in the transaction.
- **An account must never be able to strand itself.** Every page resolves a
  workspace before its handler runs, and `/welcome`, the screen that picks the
  sections for a new space, sat behind that same check. An account with no
  workspace could not reach the screen that would fix it, and signing in bounced
  `/ -> /login -> /`. Registration creates a personal workspace as part of
  signing up, and login does the same for an account that has none. The check
  is in `e2e/qa_full.py`, section U.
- **A grid cell owns the day.** `UpsertDayTotal` deletes the day's closed
  sessions for the activity and inserts one synthetic session with
  `note="timesheet"`, anchored at 09:00. Minutes `0` clears the day, and
  editing a cell replaces the sessions behind it.
- **Time comes from an injected clock.** A feature workflow may not call
  `time.Now`, `time.Since` or `time.Until`, and the architecture check fails
  the build when one does. The same holds for the process environment: only
  `cmd/paratrack` and the test fixtures may read it.
- **The CLI resolves the workspace with the lowest id.** `FirstTeamID` runs
  `SELECT id FROM teams ORDER BY id LIMIT 1` and the CLI then acts as that
  workspace's owner. It is a single-user default, not a selection mechanism.
- **`staticcheck` waits for Go 1.27.2.** v0.8.1 cannot read the export data
  that version writes ("export data version 5 is greater than maximum supported
  version 4"), and staying on Go 1.27.1 would trade that for GO-2026-6617, a
  remotely reachable HTTP/2 crash. Run it by hand with `make lint` and put the
  step back into `verify` as soon as a staticcheck release handles 1.27.2.
- **Never use `<input type="date">` or `datetime-local`.** The browser draws
  them in its own locale and no attribute moves it, which is how a Russian
  screen ended up offering «10/13/2026». Use `DateField` and `DateTimeField`
  from `web/src/components/date-field.tsx`. Two rules the component holds to: a
  value that could not be read submits nothing, not the previous one, and
  `--text-base` is 14px in this theme, so a field that has to stay at 16px on a
  phone says so through the `style` prop, because a Tailwind `text-base` class
  resolves to the same 14px token.
- **Measure colour with the transitions off.** The app animates colours, so
  anything that reads computed styles has to switch the transitions off first.
  `e2e/a11y_check.py` measured 200 ms after flipping `data-theme` and caught
  the middle of the transition, reporting 1.1:1 for the whole sidebar. Right
  after the switch the text is `oklab(0.145 0 0)`, and by 50 ms it is already
  `oklch(0.985 0 0)`. Inject
  `*,*::before,*::after{transition:none!important;animation:none!important}`
  after each navigation; `add_style_tag` does not survive a `goto`.
- **A card's title and its own body must agree.** Empty states used to be
  centred while every card title is left-aligned, so a single card read as two
  decisions stacked on top of each other. Note that the shadcn `Card` paints
  with a ring and not a border, so a detector that looks for `border-width`
  finds no cards at all and reports a clean sweep.
- **The `tasks` section is appended to any workspace with an explicit set.**
  `internal/db/schema_compat.go` runs
  `UPDATE teams SET modules = modules || ',tasks' WHERE modules <> '' AND modules <> 'none' AND modules NOT LIKE '%tasks%'`
  at startup. A workspace that never chose, or that chose `none`, keeps what it
  had.
- **The architecture check rejects history in names.** A Go file named
  `wave<N>`, an e2e file named `wave<N>`, or a test called `TestXxxWaveN` fails
  the build. The same check rejects gofmt-unformatted files, inline event
  expressions in templates, mutable state on `window` or `globalThis` in the
  browser modules, and a new internal package that has not been classified as a
  workflow, a leaf, an adapter or a special package.
- **A Go test needs Postgres.** `scripts/test.sh` starts `postgres:17-alpine`
  on port 15432 and removes it on exit. Every test that touches the database
  creates its own schema and points `search_path` at it, so the packages do not
  collide and CI runs them with `-p 8`: 693 s serial against 333 s at `-p 8` on
  this tree, 60 packages, no failures and no data races either way.

## Style / safety rails

- **A middot used as decoration is slop.** No separator between a label and its
  caption. The middot in an invoice line is document text and stays.
- **Paragraphs that restate the visible interface are slop.** Hints that carry
  meaning stay: empty states, dangerous actions, and the places where the
  interface is not self-explanatory.
- **Russian first.** The interface is Russian by default and the copy is
  written for Russian speakers, not translated from English. English is a
  second locale, and `internal/i18n/dict/en.json` holds every key.
- **Comments explain why, never what.** The comments in this codebase are good
  on that point. Keep them that way.
- **No asset from a CDN.** Everything the browser loads is embedded in the
  binary or served from `/static/`.
- **Money is integer cents.** `internal/money` owns the arithmetic, and a
  workspace rule changes the rounding before the amount is computed.

## Where the product truth lives

`PRODUCT.md` holds it. Read it before changing the behaviour of the app, and
write the change there when the behaviour changes. `docs/PRODUCT.md` holds the
business rules in detail: the definition of tracked time, the timesheet write
model, the money, the security controls and the HTTP surface.
