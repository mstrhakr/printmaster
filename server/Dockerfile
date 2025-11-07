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

# Build the binary
WORKDIR /build/server
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}' -X 'main.GitCommit=${GIT_COMMIT}' -X 'main.BuildType=release'" \
    -o printmaster-server .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 printmaster && \
    adduser -D -u 1000 -G printmaster printmaster

# Create directories
RUN mkdir -p /var/lib/printmaster/server /var/log/printmaster/server && \
    chown -R printmaster:printmaster /var/lib/printmaster /var/log/printmaster

# Copy binary from builder
COPY --from=builder /build/server/printmaster-server /usr/local/bin/printmaster-server
RUN chmod +x /usr/local/bin/printmaster-server

# Switch to non-root user
USER printmaster

# Set working directory
WORKDIR /var/lib/printmaster/server

# Expose ports
EXPOSE 9090 9443

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9090/api/health || exit 1

# Run the server
ENTRYPOINT ["/usr/local/bin/printmaster-server"]
