# paratrack: everything builds inside Docker, the host needs nothing but
# docker compose. Stages: CSS (Tailwind) -> tests + static Go binary ->
# a small runtime image.

FROM node:22-alpine AS css
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/input.css ./
# Tailwind scans these for class names (@source in input.css).
COPY internal/web/templates ../internal/web/templates
COPY internal/web/static/js/app.js ../internal/web/static/js/app.js
RUN npx tailwindcss -i ./input.css -o /out/paratrack.css --minify

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=css /out/paratrack.css internal/web/static/css/paratrack.css
ENV CGO_ENABLED=0
# A red test suite never becomes an image.
RUN go test ./...
RUN go build -trimpath -ldflags="-s -w" -o /out/paratrack ./cmd/paratrack

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/paratrack /usr/local/bin/paratrack
# SQLite (CLI / single-node mode) lives under $HOME/.track.
ENV HOME=/data
VOLUME ["/data"]
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO /dev/null http://127.0.0.1:8000/login || exit 1
ENTRYPOINT ["/usr/local/bin/paratrack", "web", "--addr", "0.0.0.0:8000"]
