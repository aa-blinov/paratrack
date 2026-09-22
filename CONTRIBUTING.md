# Contributing to paratrack

Thanks for taking the time to contribute. The project is small
enough that you won't need to wade through layers of process — the
goal of this document is to keep things that way.

## Quick start

```bash
git clone https://github.com/aa-blinov/paratrack
cd paratrack
bash scripts/setup_e2e.sh        # creates .venv + installs Playwright
make e2e-up                      # builds ./paratrack and starts the server on :8888
make e2e                         # runs the Playwright suite
make test                        # runs Go unit tests
```

`make` will print available targets via `make help` if you forget.

## Development loop

1. Branch off `master`. Prefix with `feat/`, `fix/`, `test/`, `docs/`,
   or `build/` so the commit history stays scannable.
3. Keep commits small and single-purpose. Squash noise before pushing.
4. Run `make test` + `make e2e` locally before opening a PR. CI does
   the same — PRs that fail the workflow don't get merged.
5. Push your branch, open a PR against `master`. The CI badge will
   appear in the PR timeline.

## Code style

- **Go**: `gofmt` + `go vet ./...` should be clean. Imports are
  grouped stdlib / third-party / internal. Tabs for indent.
- **Templates**: one page = one file under `internal/web/templates/`,
  composed into `base.html` via `{{define "name"}}…{{end}}`.
  Keep logic in the handlers, not the templates.
- **CSS**: single file at `internal/web/static/css/app.css`. No
  preprocessor. Use CSS variables from `:root` so light/dark mode
  stay in sync.
- **JS**: `internal/web/static/js/app.js`. Prefer declarative
  (Alpine.data + HTMX) over hand-written DOM mutation.

## Testing new features

The project ships two suites. Pick whichever fits:

* **Go unit test** — for a pure function or DB method. The
  `internal/db` package has `openTestDB(t) *DB` that returns an
  isolated `:memory:` SQLite, use it for any new DB CRUD.
* **Playwright E2E** — for anything that touches the web layer.
  `e2e/test_dashboard.py` is a single file with `check(name, ok,
  detail)` helpers; add new steps at the bottom and capture
  screenshots with `shot(page, "name")`.

Anything that wires both layers (handler + DB) needs both tests.

## Adding CLI commands

CLI dispatch lives in `cmd/paratrack/main.go:main()`. Match the
existing pattern: `runFoo(args []string)` with `flag.NewFlagSet`,
document the new command in `printUsage()`, and add a row to the
CLI reference table in `README.md`.

## Releasing

Tagging is intentionally manual for now. When cutting a release:

1. Pick a version (we follow semver).
2. Move `[Unreleased]` in `CHANGELOG.md` under a new dated heading.
3. `git tag -a vX.Y.Z -m "..."` and `git push --tags`.

The CI workflow doesn't auto-cut releases yet; that's intentional
until the project gets more users.

## Questions / ideas

Open an issue on GitHub. Keep the title specific ("Stats table
overflows on narrow phones" beats "table bug") and include a
screenshot or reproduction if it's visual.

## Code of conduct

Be kind. Disagree on substance, not on tone.