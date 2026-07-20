# syntax=docker/dockerfile:1
#
# Multi-stage Dockerfile for the Go backend.
#
# Stage 1: build the React SPA with Bun/Vite (outputs to /app/public).
# Stage 2: build the statically-linked Go binary with the SPA embedded.
# Stage 3: minimal Alpine runtime with ffmpeg + CA certs + tzdata.
#
# The Go binary is compiled with CGO_ENABLED=0 and uses modernc.org/sqlite
# (pure Go), so no C library is required at runtime. ffmpeg is installed
# because GOWA child processes rely on it for media processing.
#
# This image is intended for Go candidate tags during stabilization. The
# `latest` tag is NOT repointed here -- that is a canary promotion decision.

# ---------------------------------------------------------------------------
# Stage 1: frontend builder
# ---------------------------------------------------------------------------
FROM oven/bun:1-alpine AS frontend

WORKDIR /app

# Copy lockfiles first for better layer caching.
COPY package.json bun.lock ./
COPY client/package.json ./client/

RUN bun install --frozen-lockfile && cd client && bun install --frozen-lockfile

# Copy the frontend source and build it. Vite uses client/index.html as its
# entry point and outputs the production bundle to /app/public.
COPY client ./client

RUN cd client && bun run build

# ---------------------------------------------------------------------------
# Stage 2: Go builder
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git (some Go modules require it for VCS stamping) and certificates.
RUN apk add --no-cache git ca-certificates

# Copy the Go module files first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source.
COPY cmd ./cmd
COPY internal ./internal

# Copy the built frontend into the embed directory. The Go binary embeds
# internal/static/web via go:embed.
COPY --from=frontend /app/public ./internal/static/web

# Build a statically-linked Linux binary. TARGETARCH is provided by buildx;
# for plain `docker build` it is unset and we default to the host arch via
# the Go toolchain.
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-X github.com/fadlee/gowa-manager/internal/buildinfo.Version=${VERSION}" \
      -o /out/gowa-manager \
      ./cmd/gowa-manager-go

# ---------------------------------------------------------------------------
# Stage 3: runtime
# ---------------------------------------------------------------------------
# GOWA binaries from GitHub releases are compiled with CGO (mattn/go-sqlite3)
# and dynamically linked against glibc. Alpine uses musl libc, so a plain
# alpine:latest image cannot exec GOWA binaries — the kernel reports
# "no such file or directory" because the glibc dynamic linker is missing.
# frolvlad/alpine-glibc provides glibc on top of Alpine.
FROM frolvlad/alpine-glibc:latest AS runtime

# ffmpeg: required by GOWA child processes for media processing.
# ca-certificates: TLS for GOWA binary downloads and webhook calls.
# tzdata: timezone data for scheduling/log timestamps.
RUN apk add --no-cache ffmpeg ca-certificates tzdata

# Create a non-root user/group and a writable data directory.
RUN addgroup -S app && adduser -S -G app app \
    && mkdir -p /data \
    && chown -R app:app /data

WORKDIR /app

# Copy the statically-linked Go binary and make it executable.
COPY --from=builder /out/gowa-manager ./gowa-manager
RUN chmod +x ./gowa-manager

# /data is the volume mount point for persistent state (SQLite DB, lock file,
# downloaded GOWA binaries). It is writable by the non-root user.
VOLUME ["/data"]

EXPOSE 3000

ENV PORT=3000 \
    HOST=0.0.0.0 \
    ADMIN_USERNAME=admin \
    ADMIN_PASSWORD=password \
    DATA_DIR=/data

USER app

# Exec form so the Go binary receives signals directly (graceful SIGTERM).
ENTRYPOINT ["./gowa-manager"]
