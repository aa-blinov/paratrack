# paratrack

> Minimalist time tracker with parallel activities, advanced analytics, and a
> single-binary web UI. Pure Go, zero CGO, zero Node runtime.

![Dashboard — light](./e2e/screenshots/01-dashboard-light.png)
![Stats — inline edit + tag filter](./e2e/screenshots/tags-stats-filter-light.png)
![Graph — ECharts hour-of-day with hover tooltip](./e2e/screenshots/10-echart-tooltip.png)

## Why

Most time trackers are either web apps with 5 MB of JavaScript, or CLI tools
with no visual feedback. paratrack is both: one 20 MB Go binary gives you a
fully-interactive web UI plus the same commands on the terminal.

## Quickstart

```bash
# Build (auto-runs `make ui` — installs npm deps and compiles the
# Tailwind/DaisyUI CSS bundle into internal/web/static/css/paratrack.css)
make build

# CLI
./paratrack start reading --note "Chapter 3"
./paratrack pause reading
./paratrack status                 # active sessions + duration
./paratrack add --start "yesterday 14:00" --mode duration --duration 1h
./paratrack log --period week
./paratrack stats --period today
./paratrack goal set --activity reading --daily 2h
./paratrack tag add deep-work
./paratrack tag attach 17 deep-work

# Web UI (open http://127.0.0.1:8000)
./paratrack web --addr 127.0.0.1:8000
./paratrack web --open      # also opens the browser
```

Data lives at `~/.track/track.db` (SQLite). The schema is shared with the
original Python implementation, so you can copy a DB across if you ever need to.

## UI stack

The embedded web UI is plain Go `html/template` rendered server-side, plus a
single vendored CSS bundle (~16 KB minified) generated from Tailwind v4 +
DaisyUI v5 in `web/`. No JS framework runtime — HTMX + Alpine.js + ECharts are
vendored as static files and compiled into the binary via `go:embed`. To
tweak the design, edit `web/input.css` and run `make ui`.

## Features

- **Parallel timers** — run multiple activities simultaneously
- **Pause / resume** — accurate time accounting (no double-counting paused time)
- **Focus / switch** — pause others, start or resume the chosen one
- **Backfill** — `paratrack add --start "yesterday 09:00" --duration 1h 30m`
- **Inline edit** — change start / end / duration / note right in the stats table
- **Inline tags** — type a tag name + Enter on any session row to attach it
- **Tag filter** — `/stats?tag=deep-work` narrows breakdown + sessions
- **Goals** — per-activity daily / weekly / monthly targets with live progress
- **ECharts graph** — stacked hour-of-day bars with clickable legend
- **CSV export** — download button in the topbar
- **Light / dark / auto theme** — toggle with the button or `t` key
- **Keyboard shortcuts** — `n` new · `s` stats · `g` graph · `d` dashboard · `t` theme
- **Live timers** — active rows tick every second without server round-trips
- **Mobile-friendly** — tables collapse to stacked cards on phones
- **Hover tooltips** — graph bars show minutes per activity for that hour

## CLI reference

| Command | Aliases | Description |
|---|---|---|
| `paratrack` / `status` | `st` | List active sessions |
| `paratrack start <name>` | — | Quick-start (asks note) |
| `paratrack stop [name]` | `s` | Stop one (or all) |
| `paratrack pause [name]` | `p` | Pause one (or all) |
| `paratrack resume [name]` | `r` | Resume one (or all) |
| `paratrack focus <name>` | `sw` | Pause others, resume/start chosen |
| `paratrack add` | `a` | Backfill a session (interactive) |
| `paratrack log` | `l` | Log of closed sessions in a period |
| `paratrack stats` | — | Aggregated breakdown for a period |
| `paratrack goal` | — | Set / list / unset per-activity targets |
| `paratrack tag` | — | Add / list / attach / detach session tags |
| `paratrack web` | — | Launch embedded web UI |

All commands accept `--help`.

## Time parsing

`paratrack add`, `paratrack log`, and the inline duration input all accept a
focused subset of natural-language time:

```
now, today, yesterday, tomorrow
2026-09-22 14:00
2026-09-22T14:00:00Z
14:30
yesterday 14:00
last monday
last friday 18:00
2 hours ago, 30 min ago, 1 day ago, 2 weeks ago
```

Durations:

```
90              # 90 minutes
1h, 1.5h        # hours
30m, 90m, 45 minutes
1h 30m, 2h30m   # compound
1.5             # 1.5 minutes
```

## Architecture

```
paratrack/
├── cmd/paratrack/main.go     CLI entry, dispatch table (start/stop/goal/tag/web/...)
├── internal/
│   ├── cli/                  stdin prompt helpers (Prompt / Confirm / Choose)
│   ├── db/                   SQLite layer — pure-Go driver + schema + queries
│   │   ├── db.go             open/close helpers + time scan/format
│   │   ├── schema.go         full DDL + column / unique migrations
│   │   ├── activities.go     activity CRUD
│   │   ├── sessions.go       session CRUD + active / in-range queries
│   │   ├── goals.go          per-activity target + period progress
│   │   └── tags.go           tags + session_tags + batched hydration
│   ├── model/                domain types (Activity, Session, Tag, Goal, Reminder)
│   ├── timeparse/            NL time + duration parser (~340 LOC)
│   └── web/                  HTTP server + handlers + chart aggregation
│       ├── templates/        base + dashboard / stats / graph / goals / tags
│       └── static/           CSS, htmx.min.js, alpine.min.js, echarts.min.js, app.js (all vendored)
├── e2e/                      Playwright E2E + 25+ screenshots
└── go.mod / go.sum
```

The web UI is server-rendered HTML augmented by HTMX (targeted swaps for
forms / buttons), Alpine.js (live-ticking durations, theme toggle), and
ECharts for the graph (stacked hour-of-day bars with per-bar tooltips).
No Node, no build step, no CDN — everything is `//go:embed`-ed into the
binary.

## Testing

End-to-end (Playwright):

```bash
# one-time setup
python3 -m venv .venv
source .venv/bin/activate
pip install playwright
python -m playwright install chromium

# start the server in another terminal
./paratrack web --addr 127.0.0.1:8888

# run the suite
python e2e/test_dashboard.py
```

The script takes screenshots into `e2e/screenshots/` and prints a pass/fail
per check (currently 45/45 across dashboard, stats, graph, theme, keyboard,
duration edit, ECharts canvas + tooltip, theme-change rebuild, CSV download,
Goals CRUD, and Tags CRUD with inline attach + filter).

## Stack

- **Go 1.24+** (tested with 1.27)
- **modernc.org/sqlite** — pure-Go SQLite, no CGO
- **net/http 1.22+ ServeMux** — stdlib method routing, no router lib
- **html/template** — server rendering
- **HTMX 2.0.4** + **Alpine.js 3.14.1** — vendored, embedded
- **ECharts 5.5.1** — vendored (~1 MB), used for the graph page

## License

MIT

## Changelog

See [CHANGELOG.md](./CHANGELOG.md) for a per-release summary of what
changed.
