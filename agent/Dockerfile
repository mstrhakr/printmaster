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
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
        -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}' -X 'main.GitCommit=${GIT_COMMIT}' -X 'main.BuildType=release'" \
        -o printmaster-agent .

# Runtime stage - use distroless for minimal size
FROM gcr.io/distroless/cc-debian11:nonroot

# Copy binary from builder
COPY --from=builder /build/agent/printmaster-agent /printmaster-agent

# Distroless nonroot user is UID 65532
# Data will be stored in /var/lib/printmaster/agent (mount as volume)
WORKDIR /var/lib/printmaster/agent

# Set environment to indicate Docker environment
ENV DOCKER=true

# Expose web UI port (default 8000)
EXPOSE 8000

# Run the agent (distroless nonroot user)
ENTRYPOINT ["/printmaster-agent"]
