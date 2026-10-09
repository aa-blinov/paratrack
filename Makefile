# paratrack — common dev tasks.
#
#   make ui           install npm deps + build legacy CSS and React UI via Tailwind/Vite
#   make build        compile a /tmp/paratrack binary (auto-runs `make ui` first)
#   make install      go install into $GOBIN
#   make run          build + run the CLI (pass args via RUN=...)
#   make web          build + run the embedded web UI on :8888
#   make test         go test ./... on a throwaway Postgres (scripts/test.sh)
#   make vet          go vet ./...
#   make architecture check package dependency boundaries
#   make verify       run static, security, architecture and JS checks (no tests)
#   make e2e          Playwright suite (requires a running server on :8888)
#   make e2e-up       start the server in the background, then run e2e
#   make qa           preferred: throwaway DB + server + suite + teardown, one command
#   make clean        remove built binary + temporary server log
#   make tidy         go mod tidy
#
# Override binary path with BIN=/some/path, server address with ADDR=:9000.

GO          ?= go
NPM         ?= npm
BIN         ?= ./paratrack
ADDR        ?= 127.0.0.1:8888
SERVER_LOG  ?= /tmp/paratrack.log
STATICCHECK ?= honnef.co/go/tools/cmd/staticcheck@v0.8.1
GOVULNCHECK ?= golang.org/x/vuln/cmd/govulncheck@v1.8.0
GO_PACKAGES = $(shell GO="$(GO)" scripts/go-packages.sh)

.PHONY: ui build install run web test cover cover-html vet architecture verify lint e2e e2e-up qa stop clean tidy

ui:
	cd web && $(NPM) ci
	cd web && $(NPM) run build

build: ui
	$(GO) build -o $(BIN) ./cmd/paratrack

install:
	$(GO) install ./cmd/paratrack

run: build
	$(BIN)

web: build
	$(BIN) web --addr $(ADDR)

test:
	scripts/test.sh

cover:
	scripts/test.sh -cover ./internal/db ./internal/web ./internal/timeparse

cover-html:
	scripts/test.sh -coverprofile=coverage.out $(GO_PACKAGES)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html — open in a browser"

vet:
	$(GO) vet $(GO_PACKAGES)

architecture:
	scripts/check-architecture.sh

verify:
	$(GO) mod tidy -diff
	$(GO) vet $(GO_PACKAGES)
	$(GO) run $(GOVULNCHECK) $(GO_PACKAGES)
	$(GO) build $(GO_PACKAGES)
	scripts/check-architecture.sh
	cd web && $(NPM) run check:js

# staticcheck is out of `verify` until it can read the export data Go
# 1.27.2 writes: v0.8.1 (the newest release, and @master too) fails with
# "export data version 5 is greater than maximum supported version 4".
# Staying on Go 1.27.1 instead only trades that for a live CVE,
# GO-2026-6617, a remotely reachable HTTP/2 crash — so the toolchain moved
# and this step waited. Run it by hand with `make lint`; put the line back
# in `verify` as soon as a staticcheck release handles 1.27.2.
lint:
	$(GO) run $(STATICCHECK) $(GO_PACKAGES)

tidy:
	$(GO) mod tidy

e2e:
	@if [ ! -d .venv ]; then echo "no .venv — run scripts/setup_e2e.sh first"; exit 1; fi
	. .venv/bin/activate && python e2e/test_dashboard.py

e2e-up: build
	@pgrep -f "paratrack web --addr $(ADDR)" >/dev/null && \
	  echo "server already running" || \
	  ( $(BIN) web --addr $(ADDR) > $(SERVER_LOG) 2>&1 & echo $$! > /tmp/paratrack.pid )
	@for i in 1 2 3 4 5; do \
	  curl -sf http://$(ADDR)/ >/dev/null && break; \
	  sleep 0.5; \
	done
	@echo "server up at http://$(ADDR)/  (pid $$(cat /tmp/paratrack.pid 2>/dev/null || echo unknown))"
	@echo "run 'make e2e' to execute the suite, or 'make stop' to tear down"

stop:
	@if [ -f /tmp/paratrack.pid ]; then \
	  kill $$(cat /tmp/paratrack.pid) 2>/dev/null && rm /tmp/paratrack.pid; \
	else \
	  pkill -f "paratrack web --addr $(ADDR)"; \
	fi

# One command, nothing left behind: a throwaway Postgres, a server built from
# the working tree, the Playwright suite, then a teardown that happens whether
# the run passed, failed or was interrupted. Use this rather than e2e-up,
# which needs a server and a database prepared by hand.
qa:
	@scripts/qa-env.sh cycle

clean:
	rm -f $(BIN) $(SERVER_LOG) /tmp/paratrack.pid
