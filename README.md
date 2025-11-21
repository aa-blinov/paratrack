# better-track

Minimalist CLI time tracker with parallel activity tracking and interactive UX.

## Features

- **Quick start** — `track <activity>` сразу запускает таймер (спрашивает только заметку)
- **Parallel timers** — несколько активностей одновременно; одноимённые сессии запрещены
- **Pause/Resume** — реальные паузы с корректным учётом времени и статусом `Active/Paused`
- **Focus/Switch** — `focus`/`switch` паузят остальные и возобновляют/запускают выбранную активность
- **Retro add** — интерактивное добавление прошедших сессий через natural language (`dateparser`)
- **Log** — журнал завершённых сессий за период, клиппинг интервалов и итог
- **Stats** — сумма по активностям за период и процентные доли

## Installation

```bash
# Using uv (recommended)
uv pip install -e .

# Or with pip
pip install -e .
```

## Quick Start

```bash
# Start specific activity (quick)
uv run track reading

# Stop
uv run track stop reading

# Pause / Resume
uv run track pause reading
uv run track resume reading

# Switch / Focus (pause others)
uv run track switch work
uv run track focus work

# Status (open sessions only, HH:MM:SS)
uv run track status

# Add past session (interactive NL parsing)
uv run track add

# Log / Stats
uv run track log
uv run track stats
```

## Commands & Aliases

| Command | Alias | Description |
|---------|-------|-------------|
| `track [activity]` | — | Quick start activity (asks only note) |
| `track stop [activity]` | `s` | Stop specific or selected sessions (multiselect) |
| `track pause [activity]` | `p` | Pause specific/all active sessions |
| `track resume [activity]` | `r` | Resume specific/all paused sessions |
| `track switch <activity>` | `sw` | Pause others, resume/start selected activity |
| `track focus <activity>` | — | Same as switch |
| `track status` | `st` | Show open sessions with Status and HH:MM:SS |
| `track add` | `a` | Add past session (interactive; NL time parsing) |
| `track log` | `l` | Closed sessions for selected period with total |
| `track stats` | — | Aggregation by activity with shares |

## Web UI

Better Track now includes a simple web interface as an alternative to the CLI!

### Starting the Web UI

```bash
# Start the web server
track-web

# Or with Python
python -m track.web

# The UI will be available at http://127.0.0.1:8000

# Enable debug mode (development only)
FLASK_DEBUG=true python -m track.web
```

### Web UI Features

- **Dashboard**: View and manage active tracking sessions
  - Start new activities with optional notes
  - Pause/Resume/Stop active sessions
  - Real-time duration updates
- **Log**: View completed sessions by period (Today, Yesterday, This Week, This Month)
- **Stats**: Aggregated statistics with time shares by activity

The web UI provides the same core functionality as the CLI in a simple, user-friendly interface.

## Development

```bash
# Install with dev dependencies
uv pip install -e ".[dev]"

# Format code
uv run ruff format .

# Lint
uv run ruff check .

# Run tests
uv run pytest
```

## License

MIT
