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

echo
echo "E2E env ready. With the server running on :8888:"
echo "  source .venv/bin/activate && python e2e/test_dashboard.py"
echo "or use the Makefile:"
echo "  make e2e-up    # starts the server"
echo "  make e2e       # runs the suite"