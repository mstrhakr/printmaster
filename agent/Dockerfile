# syntax=docker/dockerfile:1.4

# Build stage (uses BuildKit cache mounts for faster dependency download and compile cache)
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy module files first so Docker layer caching can reuse downloaded modules
COPY agent/go.mod agent/go.sum ./agent/
COPY common/go.mod common/go.sum ./common/

# Use BuildKit cache mounts for Go module cache and Go build cache
# This requires Docker BuildKit enabled in CI: DOCKER_BUILDKIT=1
WORKDIR /build/agent
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    apk add --no-cache git && \
    go mod download

# Copy full source after downloading modules
WORKDIR /build
COPY agent/ ./agent/
COPY common/ ./common/

# Build the binary (do NOT use -a so the Go build cache is effective)
WORKDIR /build/agent
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
        -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}' -X 'main.GitCommit=${GIT_COMMIT}' -X 'main.BuildType=release'" \
        -o printmaster-agent .

# Runtime stage - use Alpine for minimal size with shell support for entrypoint
FROM alpine:3.21

# Install su-exec for dropping privileges (tiny, no dependencies)
RUN apk add --no-cache su-exec

# Copy binary and entrypoint from builder
COPY --from=builder /build/agent/printmaster-agent /printmaster-agent
COPY agent/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh /printmaster-agent

# Create data directory (will be chowned by entrypoint)
RUN mkdir -p /var/lib/printmaster/agent

# Data will be stored in /var/lib/printmaster/agent (mount as volume)
WORKDIR /var/lib/printmaster/agent

# Set environment to indicate Docker environment
# Default PUID/PGID for Unraid (nobody:users = 99:100)
ENV DOCKER=true \
    PUID=99 \
    PGID=100

# Expose web UI port (default 8080 HTTP, 8443 HTTPS)
EXPOSE 8080 8443

# Entrypoint handles permission setup and privilege dropping
ENTRYPOINT ["/docker-entrypoint.sh"]
