# Build stage - Frontend
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build stage - Backend
FROM golang:1.22-alpine AS backend
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -o caddypanel .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl

# Install Caddy
RUN curl -sSL "https://caddyserver.com/api/download?os=linux&arch=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')" -o /usr/local/bin/caddy \
    && chmod +x /usr/local/bin/caddy

WORKDIR /app
COPY --from=backend /app/caddypanel .

# Create data directory
RUN mkdir -p /app/data/logs /app/data/backups

# Environment defaults
ENV CADDYPANEL_PORT=8080
ENV CADDYPANEL_DATA_DIR=/app/data
ENV CADDYPANEL_CADDY_BIN=/usr/local/bin/caddy

EXPOSE 8080 80 443

VOLUME ["/app/data"]

CMD ["./caddypanel"]
