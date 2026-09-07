# Build the control plane, then ship a single static binary.
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off: modernc.org/sqlite is pure Go, so the result runs on any base image
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/agentdeck ./cmd/agentdeck

FROM debian:bookworm-slim
# git and tmux drive the LOCAL target; openssh-client reaches remote ones
RUN apt-get update && apt-get install -y --no-install-recommends \
    git tmux openssh-client curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# ttyd powers one-click terminal attach (optional but recommended)
RUN curl -fsSL -o /usr/local/bin/ttyd \
    https://github.com/tsl0922/ttyd/releases/download/1.7.7/ttyd.x86_64 \
    && chmod +x /usr/local/bin/ttyd || true

COPY --from=build /out/agentdeck /usr/local/bin/agentdeck

# NOTE: runs as root deliberately — the container drives targets over SSH with
# keys bind-mounted from the host (see deploy/docker-compose.yml) and manages
# tmux sessions for the `local` target. Harden by scoping the mounted key.
ENV AGENTDECK_DB=/data/agentdeck.db
VOLUME /data
EXPOSE 9110

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD curl -fsS "http://127.0.0.1:${AGENTDECK_PORT:-9110}/api/health" || exit 1

CMD ["agentdeck"]
