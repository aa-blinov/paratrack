# Minimal runtime image for the static Go binary.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY paratrack /usr/local/bin/paratrack
# SQLite lives under $HOME/.track — keep it on a volume.
ENV HOME=/data
VOLUME ["/data"]
EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/paratrack", "web", "--addr", "0.0.0.0:8000"]
