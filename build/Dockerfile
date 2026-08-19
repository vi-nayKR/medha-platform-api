# ============================================================
#  Medha API — Unified Dockerfile (supports Dev & CI/CD)
# ============================================================

# ── Stage 1: Base Runtime (Used by all targets) ──────────────
FROM local-registry:5000/medha-api-base:latest AS base-run

WORKDIR /app
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${SERVER_PORT:-8080}/health || exit 1

USER appuser
CMD ["./medha-api"]




# ── Stage 2: Go Compiler (Only used if compiling inside Docker)
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
# Regular layer (not a cache mount) — SSD layer cache skips this when go.mod/go.sum unchanged
RUN go mod download && \
    go install github.com/pressly/goose/v3/cmd/goose@latest
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o medha-api ./cmd/medha-api


# ── Target: dev (Standard/Local full container build) ────────
FROM base-run AS dev
COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY --from=builder --chown=appuser:appgroup /app/medha-api .
COPY --from=builder --chown=appuser:appgroup /app/migrations ./migrations


# ── Target: ci (For CI/CD using pre-built binary) ────────────
FROM base-run AS ci
COPY goose /usr/local/bin/goose
COPY --chown=appuser:appgroup migrations ./migrations
COPY --chown=appuser:appgroup medha-api .