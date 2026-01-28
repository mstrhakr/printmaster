#!/bin/sh
# Docker entrypoint for PrintMaster Server
# Ensures data directory permissions are correct before starting the server

set -e

DATA_DIR="/var/lib/printmaster/server"
LOG_DIR="/var/log/printmaster/server"

# Default to 99:100 (Unraid nobody:users) if PUID/PGID not set
PUID="${PUID:-99}"
PGID="${PGID:-100}"

# Ensure directories exist
mkdir -p "$DATA_DIR" "$LOG_DIR"

# Fix ownership if running as root (allows container to start as root and drop privileges)
if [ "$(id -u)" = "0" ]; then
    # Create user/group if they don't exist with the target PUID/PGID
    if ! getent group "$PGID" >/dev/null 2>&1; then
        addgroup -g "$PGID" printmaster 2>/dev/null || true
    fi
    if ! getent passwd "$PUID" >/dev/null 2>&1; then
        adduser -u "$PUID" -G "$(getent group "$PGID" | cut -d: -f1)" -D -H printmaster 2>/dev/null || true
    fi

    # Fix ownership of data and log directories
    chown -R "$PUID:$PGID" "$DATA_DIR" "$LOG_DIR"
    
    # Drop to unprivileged user and exec the server
    exec su-exec "$PUID:$PGID" /printmaster-server "$@"
else
    # Already running as non-root, just exec
    exec /printmaster-server "$@"
fi
