FROM python:3.13-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    git tmux openssh-client curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# ttyd for one-click terminal attach (optional but recommended)
RUN curl -fsSL -o /usr/local/bin/ttyd \
    https://github.com/tsl0922/ttyd/releases/download/1.7.7/ttyd.x86_64 \
    && chmod +x /usr/local/bin/ttyd || true

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir fastapi uvicorn asyncssh httpx pywebpush

COPY server ./server
COPY web ./web
COPY hooks ./hooks

# NOTE: runs as root deliberately — the container drives targets over SSH with
# keys bind-mounted from the host (see deploy/docker-compose.yml) and manages
# tmux sessions for the `local` target. Harden by scoping the mounted key.
ENV AGENTDECK_DB=/data/agentdeck.db
VOLUME /data
EXPOSE 9110

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD ["python", "-c", "import urllib.request,os;urllib.request.urlopen(f'http://127.0.0.1:{os.environ.get(\"AGENTDECK_PORT\",\"9110\")}/api/health')"]

CMD ["python", "-m", "server"]
