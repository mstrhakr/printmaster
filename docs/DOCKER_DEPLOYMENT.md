# PrintMaster Server - Docker Deployment

## Overview

PrintMaster Server uses **multi-stage Docker builds** with **multi-architecture support** for production-ready minimal images:

- **Distroless base (~30MB)**: Production-ready, includes CA certs, non-root user, excellent security

### Supported Architectures

All images are built for multiple architectures automatically:

| Architecture | Platform | Use Case |
|--------------|----------|----------|
| `linux/amd64` | x86_64 servers | **Most common** - Intel/AMD servers, cloud VMs |
| `linux/arm64` | ARM 64-bit | Apple Silicon, AWS Graviton, Raspberry Pi 4+ |
| `linux/arm/v7` | ARM 32-bit | Raspberry Pi 3/4 (32-bit OS), older ARM devices |

Docker automatically pulls the correct architecture for your platform.

## Image Details

**Base Image**: `gcr.io/distroless/static:nonroot`
- **Size**: ~30MB (70% smaller than Alpine-based images)
- **Security**: No shell, no package manager, minimal attack surface
- **User**: Runs as non-root user (UID 65532)
- **Contents**: CA certificates, timezone data, minimal runtime
- **Health Check**: Use external monitoring (no built-in tools)

**Image tags:**
- `latest` - Latest stable build (recommended)
- `v0.7.0` - Specific version tag

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

# Run with default settings
docker run -d \
  --name printmaster-server \
  -p 9443:9443 \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest

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
# Build distroless image (default)
docker build -t printmaster-server:latest -f server/Dockerfile .
```

## Why Minimal Images?

### Benefits

**Size Reduction**
- **Distroless**: ~30MB (70% smaller than traditional Go Alpine images)
- Faster image pulls, less bandwidth, faster deployments

**Security**
- Smaller attack surface (fewer packages = fewer CVEs)
- No shell, no package manager, no unnecessary tools
- Runs as non-root user (UID 65532)
- Reduced risk of supply chain attacks

**Performance**
- Faster container startup (less to load)
- Lower memory footprint
- Smaller storage requirements

**Trade-offs**
- No shell access (`docker exec -it` won't work for interactive debugging)
- No built-in health check tools (use external monitoring or TCP checks)
- Harder to troubleshoot inside the container (use logs and external tools)

### When to Use Distroless

**✅ Recommended for:**
- Production deployments (default choice)
- Security-conscious environments
- Cloud-native applications
- CI/CD testing
- Kubernetes/container orchestration platforms

**❌ Not ideal for:**
- Local development requiring shell access (use native binaries instead)
- Situations requiring in-container debugging (use external logging/monitoring)

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
  ghcr.io/mstrhakr/printmaster-server:latest
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
