#!/usr/bin/env bash
# Set up the Python virtualenv used by the Playwright E2E suite.
# Safe to re-run — won't touch an existing .venv unless you delete it first.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ -d .venv ]; then
  echo ".venv already exists — delete it first if you want a clean install"
  exit 0
fi

python3 -m venv .venv
. .venv/bin/activate
pip install --quiet --upgrade pip
pip install --quiet playwright
python -m playwright install chromium

# UI source: build the Tailwind/DaisyUI bundle so the embedded
# paratrack.css is fresh. Re-run `make ui` if you edit web/input.css.
if [ -d web ] && command -v npm >/dev/null 2>&1; then
  ( cd web && npm install --no-fund --no-audit )
  ( cd web && npm run build )
fi

echo
echo "E2E env ready. With the server running on :8888:"
echo "  source .venv/bin/activate && python e2e/test_dashboard.py"
echo "or use the Makefile:"
echo "  make e2e-up    # starts the server"
echo "  make e2e       # runs the suite"