# PrintMaster Server - Unraid Deployment Guide

This guide explains how to deploy PrintMaster Server on Unraid using the Docker container.

## Installation Methods

### Method 1: Using Community Applications (Recommended)

1. Install **Community Applications** plugin if not already installed
2. Search for "PrintMaster Server" in Community Applications
3. Click **Install**
4. Configure the settings (see Configuration section below)
5. Click **Apply**

### Method 2: Manual Template Installation

1. Download the template file: `server/unraid-template.xml`
2. Place it in `/boot/config/plugins/dockerMan/templates-user/`
3. Go to **Docker** tab in Unraid
4. Click **Add Container**
5. Select **PrintMaster-Server** from the template dropdown
6. Configure settings and click **Apply**

### Method 3: Manual Docker Setup

1. Go to **Docker** tab in Unraid
2. Click **Add Container**
3. Fill in the following:

```
Name: PrintMaster-Server
Repository: ghcr.io/mstrhakr/printmaster-server:latest
Network Type: Bridge
Port 9090: 9090 (TCP) - Web UI/API
Volume: /mnt/user/appdata/printmaster-server/data → /var/lib/printmaster/server
Volume: /mnt/user/appdata/printmaster-server/logs → /var/log/printmaster/server
```

## Configuration

### Essential Settings

| Setting | Value | Description |
|---------|-------|-------------|
| **HTTP Port** | `9090` | Web interface and API port |
| **Data Directory** | `/mnt/user/appdata/printmaster-server/data` | Database storage |
| **Logs Directory** | `/mnt/user/appdata/printmaster-server/logs` | Application logs |
| **Timezone** | Your timezone (e.g., `America/New_York`) | For correct log timestamps |

### Reverse Proxy Settings (Nginx Proxy Manager)

If using Nginx Proxy Manager or Swag:

| Setting | Value | Description |
|---------|-------|-------------|
| **Behind Reverse Proxy** | `true` | Tells server it's behind a proxy |
| **Use HTTPS** | `false` | Proxy handles HTTPS |
| **HTTP Port** | `9090` | Internal communication port |

**Nginx Proxy Manager Configuration:**
- Scheme: `http` (not https)
- Forward Hostname/IP: `printmaster-server` (container name) or Unraid IP
- Forward Port: `9090`
- Enable **WebSockets Support**
- SSL: Configure your SSL certificate in NPM

### Standalone HTTPS (No Reverse Proxy)

If NOT using a reverse proxy:

| Setting | Value | Description |
|---------|-------|-------------|
| **Behind Reverse Proxy** | `false` | Direct access |
| **Use HTTPS** | `true` | Enable built-in HTTPS |
| **HTTP Port** | `9090` | HTTP port |
| **HTTPS Port** | `9443` | HTTPS port (map this too!) |

You'll need to provide SSL certificates in the data directory.

### Advanced Settings

| Setting | Default | Description |
|---------|---------|-------------|
| **Log Level** | `info` | `debug`, `info`, `warn`, `error` |
| **Database Path** | `/var/lib/printmaster/server/printmaster.db` | SQLite database location |

**Note:** The container runs as UID 99 (Unraid's `nobody` user) for proper permissions.

## First-Time Setup

1. **Start the container**
   - The container will create the database and config files automatically

2. **Access the Web UI**
   - Direct: `http://YOUR-UNRAID-IP:9090`
   - Via Proxy: `https://printmaster.yourdomain.com`

3. **Configure agents**
   - Point your PrintMaster agents to the server URL
   - Agent config: `server_url = "http://YOUR-UNRAID-IP:9090"`
   - Or: `server_url = "https://printmaster.yourdomain.com"`

## File Locations

### Unraid Host
```
/mnt/user/appdata/printmaster-server/
├── data/
│   ├── printmaster.db          # SQLite database
│   └── config.toml             # Configuration (optional)
└── logs/
    └── printmaster-server.log  # Application logs
```

### Inside Container
```
/var/lib/printmaster/server/    # Data directory
/var/log/printmaster/server/    # Logs directory
```

## Networking

### Bridge Mode (Recommended)
- Use custom bridge network or default bridge
- Access via Unraid IP: `http://UNRAID-IP:9090`
- Works with Nginx Proxy Manager on same Unraid server

### Host Mode (Advanced)
- Change Network Type to `Host`
- Server binds directly to Unraid's network interface
- No port mapping needed
- Access via: `http://UNRAID-IP:9090`

### Custom Bridge Network
If you want containers to communicate by name:

```bash
# Create network (from Unraid terminal)
docker network create printmaster-net

# Add to container:
Network Type: Custom: printmaster-net
```

## Updating

### Method 1: Via Unraid UI
1. Go to **Docker** tab
2. Click **Check for Updates**
3. If update available, click **Update** button

### Method 2: Manual
1. Stop the container
2. Click **Force Update** 
3. Start the container

### Method 3: Community Applications
- Use **CA Auto Update Applications** plugin for automatic updates

## Backup

### What to Backup
```
/mnt/user/appdata/printmaster-server/data/printmaster.db
/mnt/user/appdata/printmaster-server/data/config.toml (if customized)
```

### Backup Methods

**Unraid Built-in:**
- Use **Appdata Backup** plugin
- Includes `/mnt/user/appdata/printmaster-server/` automatically

**Manual Backup:**
```bash
# Stop container first
docker stop PrintMaster-Server

# Backup database
cp /mnt/user/appdata/printmaster-server/data/printmaster.db \
   /mnt/user/backups/printmaster-$(date +%Y%m%d).db

# Restart container
docker start PrintMaster-Server
```

## Troubleshooting

### Container Won't Start
1. Check logs: Docker tab → Click container → **Logs**
2. Verify permissions on appdata folder
3. Ensure ports 9090/9443 aren't in use

### Can't Access Web UI
1. Check container is running: `docker ps | grep printmaster`
2. Verify port mapping: `docker port PrintMaster-Server`
3. Test from Unraid terminal: `curl http://localhost:9090/api/health`
4. Check firewall rules on Unraid

### Database Permission Errors
```bash
# Fix permissions (if using custom volume paths)
chown -R 99:100 /mnt/user/appdata/printmaster-server/
chmod -R 755 /mnt/user/appdata/printmaster-server/
```

**Note:** The container uses UID 99 by default, matching Unraid's `nobody` user.

### Behind Reverse Proxy - 502 Bad Gateway
- Ensure `BEHIND_PROXY=true` in container settings
- Use `http://` (not https) in Nginx Proxy Manager
- Enable WebSockets support in proxy

### View Live Logs
```bash
docker logs -f PrintMaster-Server
```

### Access Container Shell
```bash
docker exec -it PrintMaster-Server sh
```

## Performance Tuning

### For Large Deployments (1000+ Printers)

1. **Increase Docker Memory**
   - Settings → Docker → Default memory limit: 4GB+

2. **Use SSD for Database**
   - Store appdata on SSD cache drive
   - Database will be faster on SSD

3. **Adjust Log Level**
   - Set `LOG_LEVEL=warn` to reduce disk writes
   - Rotate logs more frequently

## Integration with Other Unraid Apps

### Nginx Proxy Manager
- Install from Community Applications
- Create proxy host for PrintMaster
- Use SSL certificates from Let's Encrypt

### Grafana/Prometheus (Future)
- PrintMaster may add metrics endpoint
- Can integrate for monitoring dashboards

### Uptime Kuma
- Monitor PrintMaster server availability
- Monitor URL: `http://printmaster-server:9090/api/health`

## Docker Compose (Alternative)

If you prefer docker-compose on Unraid:

```yaml
# /mnt/user/appdata/printmaster-server/docker-compose.yml
version: '3.8'

services:
  printmaster-server:
    image: ghcr.io/mstrhakr/printmaster-server:latest
    container_name: PrintMaster-Server
    restart: unless-stopped
    ports:
      - "9090:9090"
    volumes:
      - /mnt/user/appdata/printmaster-server/data:/var/lib/printmaster/server
      - /mnt/user/appdata/printmaster-server/logs:/var/log/printmaster/server
    environment:
      - SERVER_PORT=9090
      - BEHIND_PROXY=false
      - USE_HTTPS=false
      - LOG_LEVEL=info
      - TZ=America/New_York
```

Run with: `docker-compose -f /mnt/user/appdata/printmaster-server/docker-compose.yml up -d`

## Support

- GitHub Issues: https://github.com/mstrhakr/printmaster/issues
- Documentation: https://github.com/mstrhakr/printmaster/tree/main/docs
- Unraid Forums: Post in Docker Containers section

## Security Notes

1. **Network Security**
   - PrintMaster server should be on trusted network
   - Use reverse proxy with authentication if exposing to internet
   - Agents communicate over HTTP/HTTPS

2. **Data Security**
   - Database contains printer inventory data
   - No sensitive credentials stored (SNMP community strings not persisted)
   - Regular backups recommended

3. **Updates**
   - Keep container updated for security patches
   - Use `:latest` tag or pin to specific version

## Example Full Setup

```
Container Name: PrintMaster-Server
Repository: ghcr.io/mstrhakr/printmaster-server:latest
Network Type: bridge
Console Shell: sh

Ports:
  9090/tcp → 9090

Volumes:
  /mnt/user/appdata/printmaster-server/data → /var/lib/printmaster/server
  /mnt/user/appdata/printmaster-server/logs → /var/log/printmaster/server

Environment Variables:
  SERVER_PORT=9090
  BEHIND_PROXY=true
  USE_HTTPS=false
  LOG_LEVEL=info
  TZ=America/Chicago
```

**Note:** No PUID/PGID needed - container uses UID 99 by default.

This setup works with Nginx Proxy Manager handling SSL at `https://printmaster.mydomain.com`.
