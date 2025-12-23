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

# Prepare passwd/group entries so hosts that remap UID/GID (e.g., Unraid PUID/PGID 99:100)
# have known identities inside the container. We keep the distroless nonroot user (65532)
# and add a compat user/group pair for 99:100.
RUN printf "root:x:0:0:root:/root:/sbin/nologin\n" \
            "nonroot:x:65532:65532:nonroot:/home/nonroot:/sbin/nologin\n" \
            "printmaster:x:99:100:printmaster:/home/printmaster:/sbin/nologin\n" > /tmp/passwd && \
        printf "root:x:0:\n" \
            "nonroot:x:65532:\n" \
            "users:x:100:nonroot,printmaster\n" > /tmp/group && \
        mkdir -p /tmp/home/nonroot /tmp/home/printmaster

# Runtime stage - use distroless for minimal size
FROM gcr.io/distroless/cc-debian11:nonroot

# Copy binary from builder
COPY --from=builder /build/agent/printmaster-agent /printmaster-agent
# Provide passwd/group entries so running with --user 99:100 works cleanly
COPY --from=builder /tmp/passwd /etc/passwd
COPY --from=builder /tmp/group /etc/group
COPY --from=builder /tmp/home /home

# Distroless nonroot user is UID 65532
# Data will be stored in /var/lib/printmaster/agent (mount as volume)
WORKDIR /var/lib/printmaster/agent

# Set environment to indicate Docker environment
ENV DOCKER=true

# Expose web UI port (default 8080 HTTP, 8443 HTTPS)
EXPOSE 8080 8443

# Run the agent (distroless nonroot user)
ENTRYPOINT ["/printmaster-agent"]
