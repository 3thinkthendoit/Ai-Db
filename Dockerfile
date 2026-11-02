# syntax=docker/dockerfile:1

# AI-DB server image (multi-user HTTP mode, NOT the Wails desktop build).
# The web UI is compiled INTO the binary via frontend/embed.go
# (//go:embed all:dist), so there is no static directory to ship or mount.

# ---- frontend (Vite + React) ----
FROM node:22-alpine AS frontend
WORKDIR /fe
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# "npm run build" = tsc && vite build -> /fe/dist
RUN npm run build

# ---- backend (Go) ----
FROM golang:1.26-alpine AS backend
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
# dist is gitignored (and dockerignored), so it never reaches the build context;
# it must be staged next to embed.go BEFORE compiling, otherwise the go:embed
# pattern "all:dist" matches nothing and the build fails.
COPY --from=frontend /fe/dist ./frontend/dist
# Only ./cmd/server is compiled: the repo-root package main pulls in Wails and is
# the Windows desktop entrypoint, irrelevant (and partly unbuildable) here.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/aidb-server ./cmd/server

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=backend /out/aidb-server /app/aidb-server

# /app/data holds master.key (AES-GCM key for stored DB credentials) and
# session.secret (HMAC cookie key). If this directory is lost, every saved
# connection password becomes undecryptable and all sessions invalidate — always
# mount it as a named volume or bind mount in production.
ENV AIDB_ADDR=:8080
ENV AIDB_DATA_DIR=/app/data
VOLUME ["/app/data"]

# Required at run time, no sane default for production:
#   AIDB_MYSQL_DSN=user:pass@tcp(mysql:3306)/aidb?parseTime=true&charset=utf8mb4
#   AIDB_ADMIN_USERNAME / AIDB_ADMIN_PASSWORD   (bootstraps the first admin;
#   the server refuses to start with zero accounts)
#   AIDB_COOKIE_SECURE=true                     (behind HTTPS)
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:8080/api/session" || exit 1

ENTRYPOINT ["/app/aidb-server"]
