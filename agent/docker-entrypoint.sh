#!/bin/sh
# Docker entrypoint for PrintMaster Agent
# Ensures data directory permissions are correct before starting the agent

set -e

DATA_DIR="/var/lib/printmaster/agent"

# Default to 99:100 (Unraid nobody:users) if PUID/PGID not set
PUID="${PUID:-99}"
PGID="${PGID:-100}"

# Ensure data directory exists and has correct ownership
mkdir -p "$DATA_DIR"

# Fix ownership if running as root (allows container to start as root and drop privileges)
if [ "$(id -u)" = "0" ]; then
    # Create user/group if they don't exist with the target PUID/PGID
    if ! getent group "$PGID" >/dev/null 2>&1; then
        addgroup -g "$PGID" printmaster 2>/dev/null || true
    fi
    if ! getent passwd "$PUID" >/dev/null 2>&1; then
        adduser -u "$PUID" -G "$(getent group "$PGID" | cut -d: -f1)" -D -H printmaster 2>/dev/null || true
    fi

    # Fix ownership of data directory
    chown -R "$PUID:$PGID" "$DATA_DIR"
    
    # Drop to unprivileged user and exec the agent
    exec su-exec "$PUID:$PGID" /printmaster-agent "$@"
else
    # Already running as non-root, just exec
    exec /printmaster-agent "$@"
fi
