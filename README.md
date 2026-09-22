# paratrack

Minimalist time tracker with parallel activities, advanced analytics, and a single-binary web UI. Pure Go, zero CGO, zero Node runtime.

![Dashboard — light](./e2e/screenshots/01-dashboard-light.png)
![Stats — inline edit + tag filter](./e2e/screenshots/tags-stats-filter-light.png)
![Graph — ECharts hour-of-day with hover tooltip](./e2e/screenshots/10-echart-tooltip.png)

## Why

Most time trackers are either 5 MB-JS web apps or CLI tools with no visual feedback. paratrack is both: one 20 MB Go binary gives you a fully-interactive web UI plus the same commands on the terminal.

## Quickstart

```bash
make build    # auto-runs `make ui` (npm install + CSS bundle)

# CLI
./paratrack start reading --note "Chapter 3"
./paratrack pause reading
./paratrack status
./paratrack add --start "yesterday 14:00" --mode duration --duration 1h
./paratrack log --period week
./paratrack stats --period today
./paratrack goal set --activity reading --daily 2h
./paratrack tag add deep-work
./paratrack tag attach 17 deep-work

# Web UI
./paratrack web --addr 127.0.0.1:8000
./paratrack web --open
```

Data lives at `~/.track/track.db` (SQLite).

## UI stack

Server-rendered `html/template` + a single vendored CSS bundle (~16 KB minified) generated from Tailwind v4 + DaisyUI v5 in `web/`. No JS framework runtime — HTMX + Alpine.js + ECharts are vendored as static files and embedded via `go:embed`. To tweak the design, edit `web/input.css` and run `make ui`.

## Features

- Parallel timers, pause / resume, focus / switch
- Backfill via natural-language time
- Inline edit of start / end / duration / note in the stats table
- Inline tags: type + Enter on any session row
- Tag filter (`/stats?tag=deep-work`)
- Per-activity goals (daily / weekly / monthly) with live progress
- ECharts graph: stacked hour-of-day bars, clickable legend
- CSV export
- Light / dark / auto theme; toggle with the button or `t` key
- Keyboard shortcuts: `n` new · `s` stats · `g` graph · `d` dashboard · `t` theme
- Live-ticking durations
- Mobile-friendly tables (collapse to cards on phones)
- Hover tooltips on graph bars

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

`paratrack add`, `paratrack log`, and the inline duration input accept:

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
├── cmd/paratrack/main.go     CLI dispatch
├── internal/
│   ├── cli/                  stdin prompt helpers
│   ├── db/                   SQLite layer (modernc.org/sqlite, pure-Go)
│   ├── model/                domain types
│   ├── timeparse/            NL time + duration parser
│   └── web/                  HTTP server + handlers + chart aggregation
│       ├── templates/        base + 5 pages
│       └── static/           vendored CSS, htmx, alpine, echarts, app.js
├── web/                      Tailwind + DaisyUI source (`make ui` builds it)
├── e2e/                      Playwright suite
└── go.mod / go.sum
```

The web UI is server-rendered HTML augmented by HTMX (targeted swaps), Alpine.js (live-ticking durations, theme toggle), and ECharts (graph). No Node, no build step, no CDN — everything is `//go:embed`-ed.

## Testing

End-to-end (Playwright):

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install playwright
python -m playwright install chromium

./paratrack web --addr 127.0.0.1:8888 &
python e2e/test_dashboard.py    # 45/45
```

Go unit tests:

```bash
go test ./... -race
```

## Stack

- **Go 1.27** — tested
- **modernc.org/sqlite** — pure-Go, no CGO
- **net/http 1.22+** — stdlib method routing
- **html/template** — server rendering
- **HTMX 2.0.4** + **Alpine.js 3.14.1** — vendored
- **ECharts 5.5.1** — vendored (~1 MB)
- **Tailwind v4** + **DaisyUI v5** — CSS source in `web/`

## License

MIT

## Changelog

See [CHANGELOG.md](./CHANGELOG.md).
