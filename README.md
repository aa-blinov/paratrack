# paratrack

Time tracking that turns into an invoice. Several timers run at once, the week lands in a timesheet, and the same minutes become a client invoice or a team payout. It works in the browser and on a phone, and one `docker compose` puts it on your own server.

[Site](https://aa-blinov.github.io/paratrack/) | [Documentation](https://aa-blinov.github.io/paratrack/docs/) | [Open the service](https://paratrack.duckdns.org/register)

![The dashboard: three timers run at once, and the block of unbilled work sits below](site/assets/shots/dashboard.webp)

[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![CI](https://github.com/aa-blinov/paratrack/actions/workflows/ci.yml/badge.svg)](https://github.com/aa-blinov/paratrack/actions/workflows/ci.yml)

---

## Quick start

Docker and docker compose are all the machine needs. The image builds the CSS, runs the Go test suite against a temporary Postgres of its own, and compiles the binary; a Postgres container sits beside it and holds the data.

```bash
git clone https://github.com/aa-blinov/paratrack
cd paratrack
cp .env.example .env   # set POSTGRES_PASSWORD and PARATRACK_SECRET_KEY
docker compose up -d --build
# -> http://127.0.0.1:8000/register
```

The data lives in Postgres and nowhere else, in the `pgdata` volume. The schema is created and migrated at startup. Mail, single sign-on, Stripe, a domain with HTTPS, backups to S3 and updates are described in the documentation: [Your own server](https://aa-blinov.github.io/paratrack/docs/self-host/).

Without Docker, Go 1.27 and any Postgres 17 will do:

```bash
export PARATRACK_DATABASE_URL='postgres://postgres:dev@127.0.0.1:5432/postgres?sslmode=disable'
export PARATRACK_ENV=development PARATRACK_PUBLIC_URL=http://127.0.0.1:8000
make build    # builds the CSS and the React bundle (npm ci + Tailwind + Vite)
./paratrack web --addr 127.0.0.1:8000
```

The CLI works against the same database and resolves the workspace with the lowest id:

```bash
paratrack start layout      # start a timer
paratrack stop              # stop it
paratrack stats             # the numbers for today
paratrack help              # every command
```

## What it does

- **Several timers at once.** Pause one, pause all, or stop all with a confirmation. Backdated entries take free text: "yesterday 14:00", "2 hours ago". A running timer is visible on every page and on the phone.
- **Time broken down.** Statistics by project, activity, person and tag, with a session editable in the table itself. The weekly timesheet, minutes by hour of the day, goals for the day, the week and the month, saved reports.
- **Money from the tracked minutes.** A client invoice built from tracked time: PDF, a completion act, an email with the file attached, online payment through Stripe, and a "My tax" receipt. Each line's time is rounded to 0.01 h (36 s) and priced from the rounded hours, so hours × rate = amount holds to the cent. Workspace rules cover rounding to 6, 15, 30 or 60 minutes, the invoice number prefix and the logo. Payouts to members at their own rates, a weekly load plan for the team, five ready reports in HTML and CSV.
- **A team, when there is one.** Workspaces, the roles owner, admin and member, invitations by link, transfer of ownership. A member sees only their own time, and client rates stay with the managers. A manager approves a timesheet week, and a payout counts approved time only.
- **Set up your way.** At registration you pick a set of sections: "For myself", "Freelancer", "Studio". The duration format, the first day of the week, the time zone, the dashboard blocks, the menu sections and the bottom bar on the phone are yours to change.
- **The rest of the tools.** Tasks from GitHub, GitLab, Jira, Trello, Asana, ClickUp, Todoist and Notion, each with a one-click timer start. History imported from Toggl, Harvest and Clockify. A REST API with tokens and pagination, signed webhooks, an audit log, OIDC single sign-on, a Chrome extension and a CLI.
- **Phone and offline.** It installs on the home screen as an app. Without a network the actions queue up and go out later, stamped with the moment you pressed the button. Push notifications about your own events.

The interface is Russian, and English is there too. Light and dark themes.

## Screenshots

| The timesheet | An invoice |
| :---: | :---: |
| ![The timesheet: activities in rows, days in columns, minutes in a cell](site/assets/shots/timesheet.webp) | ![An invoice: the requisites, the lines of work, the total](site/assets/shots/invoice.webp) |

| Statistics for a week | The dark theme |
| :---: | :---: |
| ![Statistics for a week: the breakdown, the filters, editing in place](site/assets/shots/stats.webp) | ![The dashboard in the dark theme](site/assets/shots/dashboard-dark.webp) |

The screenshots are taken on demo data.

## How it works

```
browser -> internal/web --+
                          +-> feature workflows -> internal/db -> PostgreSQL 17
CLI ----> internal/cli ---+
```

One Go binary, no CGO. The full-page screens and the shared navigation shell are React 19 on shadcn/ui. Go templates remain for utility pages and HTML fragments, and HTMX with Alpine.js handle individual actions and fragment refreshes. The React bundle and the stylesheets are embedded in the binary with `go:embed`, and no asset is fetched from a CDN.

| Path | What it holds |
| --- | --- |
| `cmd/paratrack/` | The process root: configuration, the service graph, the lifecycle. |
| `internal/web/` | The HTTP adapter: routes, view models, templates, the JSON API. |
| `internal/cli/` | The CLI adapter: command parsing and terminal output. |
| `internal/appmodel/` | Application commands and results that both transports share. |
| `internal/<feature>/` | The feature workflows: auth, teams, tracking, invoicing, payroll, scheduling, reporting, webhooks, push. |
| `internal/db/` | Postgres: the schema, the migrations, the queries. |
| `internal/model/` | Domain types that know nothing about a transport. |
| `web/` | The React and Tailwind sources, built by `make ui`. |
| `site/` | The landing page and the user documentation, published on GitHub Pages. |
| `extension/` | The Chrome extension (MV3). |
| `e2e/` | The Playwright checks. |
| `docs/` | ARCHITECTURE, PRODUCT, QA, JOURNEY and the screen notes. |

Every page and every `/api/*` call goes through `requireAuth`: a session cookie or an `Authorization: Bearer pt_…` token. Every read is scoped to the workspace, and for a member it is scoped to their own sessions as well.

Money is integer cents. A line's time is rounded to hundredths of an hour and priced from the rounded value, so the total on the screen, in the PDF and in the payout agree.

The dependency map, the resource ownership and the rules between the layers are in [docs/architecture.md](docs/architecture.md). The SQL lives in `internal/db/schema.sql`, and the startup migrations and the compatibility with older databases belong to `internal/db`.

## Configuration

The compose app reads a `.env` file. [.env.example](.env.example) holds the whole list with the values filled in.

| Variable | Default | What it does |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | none | The password of the Postgres container. Required. |
| `PARATRACK_DATABASE_URL` | none | The Postgres connection string. Required. |
| `PARATRACK_ENV` | `production` | `development` and `test` relax the secret-key and the HTTPS rules. |
| `PARATRACK_PUBLIC_URL` | none | The canonical origin for reset, invite, SSO and payment links. Required outside development and test, and it has to be `https` there. |
| `PARATRACK_SECRET_KEY` | none | Encrypts integration tokens, Stripe keys and webhook secrets at rest (AES-256-GCM). Required outside development and test, and losing it means re-entering those secrets. |
| `PARATRACK_TZ` | `Europe/Moscow` | The time zone for a visitor whose browser has not reported one. |
| `PARATRACK_TRUSTED_PROXIES` | none | Comma-separated CIDR ranges allowed to set forwarded headers. |
| `PARATRACK_PORT` | `8000` | The host port the compose app answers on. |
| `PARATRACK_SENTRY_DSN` | none | Sentry error reporting. Empty means off, and nothing leaves the server. |
| `PARATRACK_SENTRY_TRACES` | `1.0` | The trace sample rate, from 0 to 1. |
| `PARATRACK_SMTP_HOST` | none | The SMTP server for invoices, invites and password resets. Empty means the letter goes to the server log. |
| `PARATRACK_SMTP_USER` / `PARATRACK_SMTP_PASS` | none | The SMTP credentials. |
| `PARATRACK_MAIL_FROM` | none | The From address. |
| `PARATRACK_OIDC_ISSUER` | none | The single sign-on issuer URL. |
| `PARATRACK_OIDC_CLIENT_ID` / `PARATRACK_OIDC_CLIENT_SECRET` | none | The single sign-on credentials. All three or none. |
| `PARATRACK_OIDC_TRUST_EMAIL` | unset | `1` trusts an IdP that omits `email_verified`. |
| `PARATRACK_STRIPE_KEY` | none | The fallback Stripe key for a workspace that has none. |
| `PARATRACK_STRIPE_WEBHOOK_SECRET` | none | The fallback Stripe webhook secret. |
| `PARATRACK_WEBHOOK_ALLOW_PRIVATE` | unset | `1` lets outbound webhooks reach private addresses. |
| `PARATRACK_INTEGRATION_ALLOW_PRIVATE` | unset | `1` lets the Jira and GitLab integrations reach private addresses. |
| `PARATRACK_JIRA_SITE` / `PARATRACK_GITLAB_SITE` | none | The default site for those two providers. |

## Development

```bash
make ui            # npm ci, then the Tailwind CSS and the React bundle
make build         # compile ./paratrack, with `make ui` first
make test          # scripts/test.sh: go test on a throwaway Postgres in docker
make vet           # go vet
make verify        # tidy -diff, vet, govulncheck, build, the architecture check
make architecture  # scripts/check-architecture.sh
make web           # build and serve the embedded UI on 127.0.0.1:8888
make e2e           # the Playwright suite, against a server on :8888
make e2e-up        # start that server in the background
make qa            # throwaway database, server, suite and teardown, one command
```

Go 1.27.2, Node 22 and Postgres 17. The browser suite needs a Python virtualenv:

```bash
bash scripts/setup_e2e.sh      # .venv, Playwright and chromium
scripts/test.sh                 # every Go test on a throwaway Postgres
scripts/test.sh -run TestName ./internal/web
```

| Path | What it holds |
| --- | --- |
| `internal/web/routes_pages.go` | The page routes and the JSON routes under `/api`. |
| `internal/app/` | The service graph the process root builds. |
| `internal/db/schema.sql` | The SQL schema. |
| `internal/db/migrations.go` | The startup migrations. |
| `scripts/architecture/` | The dependency and boundary rules the architecture check enforces. |
| `web/src/` | The React screens and the Tailwind entry point. |
| `e2e/` | The Playwright suite and the QA scripts. |

CI runs four jobs on every push and pull request: the UI build with the typecheck and the React unit tests, the static and architecture gates, the race-enabled Go tests on Postgres, and the Playwright suite against a freshly built server. The site in `site/` goes out to GitHub Pages from a separate workflow when it changes.

The documents beside this file: [docs/PRODUCT.md](docs/PRODUCT.md) for the capabilities and the money and time rules, [docs/architecture.md](docs/architecture.md) for the packages, [docs/QA.md](docs/QA.md) for the check matrix, [docs/JOURNEY.md](docs/JOURNEY.md) for the screen map, [DESIGN.md](DESIGN.md) for the design system, [CONTRIBUTING.md](CONTRIBUTING.md) and [CHANGELOG.md](CHANGELOG.md).

## License

MIT, © 2026 paratrack contributors. See [LICENSE](LICENSE).
