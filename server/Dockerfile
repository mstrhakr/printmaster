# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY server/go.mod server/go.sum ./server/
COPY common/go.mod ./common/

# Download dependencies
WORKDIR /build/server
RUN go mod download

# Copy source code
WORKDIR /build
COPY server/ ./server/
COPY common/ ./common/

# Build the binary with static linking and optimizations
WORKDIR /build/server
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -installsuffix cgo \
    -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}' -X 'main.GitCommit=${GIT_COMMIT}' -X 'main.BuildType=release'" \
    -o printmaster-server .

# Runtime stage - use distroless for minimal size with ca-certs and timezone data
# Alternative: Use scratch for absolute minimal (but no shell, harder to debug)
FROM gcr.io/distroless/static:nonroot

# Copy binary from builder
COPY --from=builder /build/server/printmaster-server /printmaster-server

# Distroless nonroot user is UID 65532
# Data will be stored in /var/lib/printmaster/server (mount as volume)
WORKDIR /var/lib/printmaster/server

# Set environment to indicate Docker environment
ENV DOCKER=true

# Expose ports
EXPOSE 9090 9443

# Note: Distroless doesn't support HEALTHCHECK with wget/curl
# Health checks should be done externally (docker-compose, k8s, etc.)

# Run the server (distroless nonroot user)
ENTRYPOINT ["/printmaster-server"]
