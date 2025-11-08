# PrintMaster Server - Docker Deployment

## Overview

PrintMaster Server uses **multi-stage Docker builds** with **multi-architecture support** for minimal image sizes:

- **Default**: Distroless base (~30MB) - Production-ready, includes CA certs, non-root user
- **Scratch**: Ultra-minimal (~10MB) - Absolute smallest, debugging harder
- **Alpine**: Debug-friendly (~100MB) - Includes shell and tools

### Supported Architectures

All images are built for multiple architectures automatically:

| Architecture | Platform | Use Case |
|--------------|----------|----------|
| `linux/amd64` | x86_64 servers | **Most common** - Intel/AMD servers, cloud VMs |
| `linux/arm64` | ARM 64-bit | Apple Silicon, AWS Graviton, Raspberry Pi 4+ |
| `linux/arm/v7` | ARM 32-bit | Raspberry Pi 3/4 (32-bit OS), older ARM devices |

Docker automatically pulls the correct architecture for your platform.

## Image Variants

| Dockerfile | Base Image | Size | Shell | Health Check | Use Case |
|------------|-----------|------|-------|--------------|----------|
| `Dockerfile` (default) | `distroless/static:nonroot` | ~30MB | ❌ | External only | **Production (recommended)** |
| `Dockerfile.scratch` | `scratch` | ~10MB | ❌ | None | Ultra-minimal production |
| `Dockerfile.alpine` | `alpine:latest` | ~100MB | ✅ | Built-in | Development/debugging |

**Image tags:**
- `latest` - Latest distroless build (default)
- `latest-scratch` - Latest scratch build (ultra-minimal)
- `v0.7.0` - Specific version (distroless)
- `v0.7.0-scratch` - Specific version (scratch)

## Quick Start

### Using Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/mstrhakr/printmaster.git
cd printmaster/server

# Start the server
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the server
docker-compose down
```

### Using Pre-built Image from GitHub Container Registry

```bash
# Pull the latest image (distroless, auto-detects architecture)
docker pull ghcr.io/mstrhakr/printmaster-server:latest

# Or pull the scratch variant
docker pull ghcr.io/mstrhakr/printmaster-server:latest-scratch

# Run with default settings (distroless base)
docker run -d \
  --name printmaster-server \
  -p 9443:9443 \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest

# Run the scratch variant (smallest)
docker run -d \
  --name printmaster-server \
  -p 9443:9443 \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest-scratch

# Run behind reverse proxy (HTTP mode)
docker run -d \
  --name printmaster-server \
  -p 9090:9090 \
  -e BEHIND_PROXY=true \
  -e BIND_ADDRESS=0.0.0.0 \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest
```

### Building Custom Images

```bash
# Default distroless build
docker build -t printmaster-server:distroless -f server/Dockerfile .

# Scratch build (ultra-minimal)
docker build -t printmaster-server:scratch -f server/Dockerfile.scratch .

# Alpine build (with debugging tools) - if you create this variant
docker build -t printmaster-server:alpine -f server/Dockerfile.alpine .
```

## Why Minimal Images?

### Benefits

**Size Reduction**
- **Distroless**: ~30MB (99MB smaller than traditional Go Alpine images)
- **Scratch**: ~10MB (absolute minimum, just the binary)
- Faster image pulls, less bandwidth, faster deployments

**Security**
- Smaller attack surface (fewer packages = fewer CVEs)
- Distroless: No shell, no package manager, no unnecessary tools
- Scratch: Literally nothing except your binary
- Reduced risk of supply chain attacks

**Performance**
- Faster container startup (less to load)
- Lower memory footprint
- Smaller storage requirements

**Trade-offs**
- **Distroless/Scratch**: No shell access (`docker exec -it` won't work)
- **Distroless/Scratch**: No built-in health check tools (use external monitoring)
- **Distroless**: Harder to debug (but safer in production)

### When to Use Each

| Situation | Recommended Image |
|-----------|------------------|
| Production deployment | **Distroless** (default) |
| Production, size-critical | **Scratch** |
| Development/debugging | **Alpine** (if created) |
| CI/CD testing | **Distroless** |
| Need shell access | **Alpine** (create custom) |

## Multi-Architecture Support

### Automatic Architecture Detection

Docker automatically pulls the correct image for your platform:

```bash
# On x86_64 server - pulls linux/amd64 image
docker pull ghcr.io/mstrhakr/printmaster-server:latest

# On Raspberry Pi 4 (64-bit) - pulls linux/arm64 image
docker pull ghcr.io/mstrhakr/printmaster-server:latest

# On Raspberry Pi 3 (32-bit) - pulls linux/arm/v7 image
docker pull ghcr.io/mstrhakr/printmaster-server:latest
```

### Platform-Specific Examples

**Raspberry Pi 4 (64-bit OS):**
```bash
# Automatically uses arm64 image
docker run -d \
  --name printmaster-server \
  -p 9443:9443 \
  -v /mnt/usb/printmaster:/var/lib/printmaster/server \
  --restart unless-stopped \
  ghcr.io/mstrhakr/printmaster-server:latest-scratch
```

**AWS Graviton (ARM-based servers):**
```bash
# Automatically uses arm64 image
docker run -d \
  --name printmaster-server \
  -p 9090:9090 \
  -e BEHIND_PROXY=true \
  -v /data/printmaster:/var/lib/printmaster/server \
  --restart unless-stopped \
  ghcr.io/mstrhakr/printmaster-server:latest
```

**Manual Platform Override (if needed):**
```bash
# Force specific architecture (rarely needed)
docker pull --platform linux/arm64 ghcr.io/mstrhakr/printmaster-server:latest
docker pull --platform linux/amd64 ghcr.io/mstrhakr/printmaster-server:latest
```

### Verify Architecture

```bash
# Check which architecture you pulled
docker image inspect ghcr.io/mstrhakr/printmaster-server:latest | grep Architecture

# Expected output:
# "Architecture": "arm64"   (on ARM systems)
# "Architecture": "amd64"   (on x86 systems)
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_HTTP_PORT` | `9090` | HTTP port |
| `SERVER_HTTPS_PORT` | `9443` | HTTPS port |
| `BEHIND_PROXY` | `false` | Run in reverse proxy mode |
| `PROXY_USE_HTTPS` | `false` | Use HTTPS even behind proxy |
| `BIND_ADDRESS` | `0.0.0.0` | Bind address |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `TLS_MODE` | `self-signed` | TLS mode (self-signed, custom, letsencrypt) |
| `DB_PATH` | `/var/lib/printmaster/server/printmaster.db` | Database path |

### Custom Configuration File

Create a `config.toml` and mount it:

```bash
docker run -d \
  --name printmaster-server \
  -p 9443:9443 \
  -v $(pwd)/config.toml:/var/lib/printmaster/server/config.toml:ro \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest
```

### Custom TLS Certificates

```bash
docker run -d \
  --name printmaster-server \
  -p 9443:9443 \
  -e TLS_MODE=custom \
  -e TLS_CERT_PATH=/certs/server.crt \
  -e TLS_KEY_PATH=/certs/server.key \
  -v $(pwd)/certs:/certs:ro \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest
```

## Reverse Proxy Setup

### Nginx Proxy Manager

1. Add a new Proxy Host
2. **Details tab:**
   - Domain Names: `printmaster.example.com`
   - Scheme: `http` (or `https` if using dual-TLS)
   - Forward Hostname/IP: `printmaster-server` (container name) or IP
   - Forward Port: `9090` (or `9443` for dual-TLS)
   - Enable Websockets: ✓

3. **SSL tab:**
   - SSL Certificate: (your choice)
   - Force SSL: ✓
   - HTTP/2 Support: ✓

4. **Advanced tab (optional):**
```nginx
# For HTTP backend (proxy_use_https=false)
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header Host $host;
```

### Docker Compose with Nginx

```yaml
version: '3.8'

services:
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./certs:/etc/nginx/certs:ro
    depends_on:
      - printmaster-server
    networks:
      - printmaster-network

  printmaster-server:
    image: ghcr.io/mstrhakr/printmaster-server:latest
    environment:
      - BEHIND_PROXY=true
      - BIND_ADDRESS=0.0.0.0
    volumes:
      - printmaster-data:/var/lib/printmaster/server
    networks:
      - printmaster-network

volumes:
  printmaster-data:

networks:
  printmaster-network:
```

## Building from Source

```bash
# Build the image
docker build -t printmaster-server:local \
  --build-arg VERSION=0.3.4 \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
  -f server/Dockerfile .

# Run your custom build
docker run -d -p 9443:9443 printmaster-server:local
```

## Troubleshooting

### View Logs
```bash
docker logs printmaster-server
docker logs -f printmaster-server  # Follow logs
```

### Access Container Shell
```bash
docker exec -it printmaster-server sh
```

### Check Health
```bash
docker inspect printmaster-server | grep Health -A 10
curl http://localhost:9090/api/health
```

### Persist Data
Always use volumes to persist database and logs:
```bash
-v printmaster-data:/var/lib/printmaster/server
-v printmaster-logs:/var/log/printmaster/server
```

## Security

- Container runs as non-root user (`printmaster` UID 1000)
- Read-only config mount recommended
- Use custom TLS certificates in production
- Keep images updated: `docker pull ghcr.io/mstrhakr/printmaster-server:latest`

## Updates

```bash
# Pull latest image
docker pull ghcr.io/mstrhakr/printmaster-server:latest

# Recreate container
docker-compose down
docker-compose up -d
```
