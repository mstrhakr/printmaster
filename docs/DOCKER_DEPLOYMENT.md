# PrintMaster Server - Docker Deployment

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
# Pull the latest image
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
