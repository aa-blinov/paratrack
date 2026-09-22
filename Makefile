# paratrack — common dev tasks.
#
#   make build        compile a /tmp/paratrack binary
#   make install      go install into $GOBIN
#   make run          build + run the CLI (pass args via RUN=...)
#   make web          build + run the embedded web UI on :8888
#   make test         go test ./...
#   make vet          go vet ./...
#   make e2e          Playwright suite (requires a running server on :8888)
#   make e2e-up       start the server in the background, then run e2e
#   make clean        remove built binary + temporary server log
#   make tidy         go mod tidy
#
# Override binary path with BIN=/some/path, server address with ADDR=:9000.

GO          ?= go
BIN         ?= ./paratrack
ADDR        ?= 127.0.0.1:8888
SERVER_LOG  ?= /tmp/paratrack.log

.PHONY: build install run web test cover vet e2e e2e-up clean tidy

build:
	$(GO) build -o $(BIN) ./cmd/paratrack

install:
	$(GO) install ./cmd/paratrack

run: build
	$(BIN)

web: build
	$(BIN) web --addr $(ADDR)

test:
	$(GO) test ./...

cover:
	$(GO) test -cover ./internal/db ./internal/web ./internal/timeparse

cover-html: cover
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html — open in a browser"

vet:
	$(GO) vet ./...

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

clean:
	rm -f $(BIN) $(SERVER_LOG) /tmp/paratrack.pid