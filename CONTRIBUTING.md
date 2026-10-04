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

## Development loop

1. Branch off `master`. Prefix with `feat/`, `fix/`, `test/`, `docs/`,
   or `build/` so the commit history stays scannable.
2. Keep commits small and single-purpose. Squash noise before pushing.
3. Before the first local verification, install frontend tooling with
   `cd web && npm ci`. Then run `make verify`, `make test` and `make e2e` before
   opening a PR. `make verify` runs static, security, architecture and
   frontend checks without executing tests. CI installs these dependencies
   itself and runs the same static gates plus race-enabled Go tests and the
   browser suite.
4. Push your branch, open a PR against `master`. The CI badge will
   appear in the PR timeline.

## Code style

- **Go**: `gofmt` + `go vet ./...` should be clean. Imports are
  grouped stdlib / third-party / internal. Tabs for indent.
- **Templates**: pages live under `internal/web/templates/` and are composed
  through the shared layout. Keep data preparation in handlers/view models and
  presentation in templates.
- **CSS**: edit Tailwind/DaisyUI sources in `web/` and rebuild the embedded
  bundle with `npm run build` from that directory. Use shared design tokens
  for light and dark themes.
- **JS**: `internal/web/static/js/app.js` is the entrypoint; feature behavior
  lives in focused `app-*.js` ES modules. Use the shared request/offline APIs
  and imports/exports instead of mutable globals. Keep event handlers in the
  modules and use data attributes in templates; inline event expressions are
  rejected by the architecture check. Run `npm run check:js` in `web/` after
  JavaScript changes.

## Testing new features

The project ships two suites. Pick whichever fits:

* **Go unit test** — for a pure function or DB method. The
  `internal/db` package has `openTestDB(t) *DB` that returns an
  isolated Postgres schema (other packages: `testutil.OpenTest(t)`), use it
  for any new DB CRUD. Run everything with `scripts/test.sh`.
* **Playwright E2E** — for anything that touches the web layer.
  `e2e/test_dashboard.py` is a single file with `check(name, ok,
  detail)` helpers; add new steps at the bottom and capture
  screenshots with `shot(page, "name")`.

Anything that wires both layers (handler + DB) needs both tests.

## Adding CLI commands

CLI process setup lives in `cmd/paratrack/main.go`; command dispatch and
command implementations live in `internal/cli`. Add a `runFoo(rt *Runtime,
args []string)` command, wire it into `RunWithRuntime`, document it in
`printUsage`, and add a row to the CLI reference table in `README.md`. Keep
process configuration and concrete service construction in the command root.

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
