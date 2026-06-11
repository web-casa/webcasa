# Build stage - Frontend
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build stage - Backend
FROM golang:1.26-alpine AS backend
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
# seed_apps.json.gz is pre-generated and committed to the repo.
# Run "make seed-data" to regenerate it before building a release.
RUN CGO_ENABLED=1 go build -o webcasa .

# Runtime stage
FROM alpine:3.19
# libcap provides setcap (privileged-port binding as non-root); su-exec drops
# privileges from the entrypoint after fixing volume ownership.
RUN apk add --no-cache ca-certificates curl bash libcap su-exec

# Install Caddy — pinned version + mandatory SHA256 verification.
# Keep CADDY_VERSION in sync with the VERSIONS file used by install.sh.
ARG CADDY_VERSION=2.11.2
RUN set -eux; \
    arch="$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')"; \
    asset="caddy_${CADDY_VERSION}_linux_${arch}.tar.gz"; \
    base="https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}"; \
    curl -fsSL "${base}/${asset}" -o /tmp/caddy.tar.gz; \
    curl -fsSL "${base}/caddy_${CADDY_VERSION}_checksums.txt" -o /tmp/caddy_checksums.txt; \
    grep " ${asset}\$" /tmp/caddy_checksums.txt | sed "s| ${asset}\$| /tmp/caddy.tar.gz|" | sha256sum -c -; \
    tar -xzf /tmp/caddy.tar.gz -C /tmp caddy; \
    install -m 0755 /tmp/caddy /usr/local/bin/caddy; \
    rm -f /tmp/caddy.tar.gz /tmp/caddy_checksums.txt /tmp/caddy; \
    # Allow Caddy to bind :80/:443 without running as root
    setcap 'cap_net_bind_service=+ep' /usr/local/bin/caddy

WORKDIR /app
COPY --from=backend /app/webcasa .
COPY --from=backend /app/web/dist ./web/dist

# Create unprivileged runtime user and owned data/Caddy dirs.
# Caddy stores certs/data under $XDG_DATA_HOME and $XDG_CONFIG_HOME; point both
# at /app so nothing is written under /root and the volumes are user-owned.
RUN adduser -S -D -H -h /app webcasa \
    && mkdir -p /app/data/logs /app/data/backups /app/caddy/data /app/caddy/config \
    && chown -R webcasa /app/data /app/caddy

# Environment defaults
ENV WEBCASA_PORT=8080
ENV WEBCASA_DATA_DIR=/app/data
ENV WEBCASA_CADDY_BIN=/usr/local/bin/caddy
ENV XDG_DATA_HOME=/app/caddy/data
ENV XDG_CONFIG_HOME=/app/caddy/config

EXPOSE 8080 80 443

VOLUME ["/app/data"]

# Entrypoint runs as root only long enough to chown the data/Caddy volumes —
# pre-existing volumes from the old root-based image (or named volumes upgraded
# in place) are root-owned and otherwise unreadable by the unprivileged user —
# then drops to `webcasa` via su-exec. The process itself never runs as root.
RUN printf '%s\n' \
    '#!/bin/sh' \
    'set -e' \
    'chown -R webcasa /app/data /app/caddy 2>/dev/null || true' \
    'exec su-exec webcasa "$@"' \
    > /usr/local/bin/entrypoint.sh \
    && chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["./webcasa"]
