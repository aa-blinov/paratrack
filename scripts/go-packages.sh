#!/bin/sh
# List Go packages owned by this repository. Frontend dependencies may ship
# source files for other languages, including Go, under web/node_modules.
set -eu
cd "$(dirname "$0")/.."
go_bin=${GO:-go}
"$go_bin" list -f '{{.ImportPath}}' ./... | awk 'index($0, "/node_modules/") == 0'
