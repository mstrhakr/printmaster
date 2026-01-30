#!/bin/bash
# Seed E2E test databases with test data
#
# This script seeds test data into EXISTING databases that were created
# by the application containers on startup. This avoids schema duplication.
#
# Usage: ./seed-testdata.sh
#
# Requirements: 
#   - sqlite3 must be installed
#   - Containers must be running with initialized databases

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTDATA_DIR="${SCRIPT_DIR}/testdata"
SEED_DIR="${TESTDATA_DIR}/seed"

echo "=== PrintMaster E2E Test Data Seeder ==="
echo ""

# Check for sqlite3
if ! command -v sqlite3 &> /dev/null; then
    echo "ERROR: sqlite3 is required but not installed."
    echo "Install with:"
    echo "  Ubuntu/Debian: sudo apt install sqlite3"
    echo "  macOS: brew install sqlite3"
    echo "  Windows: download from https://sqlite.org/download.html"
    exit 1
fi

# Create directories if needed (containers will create DBs here)
mkdir -p "${TESTDATA_DIR}/server"
mkdir -p "${TESTDATA_DIR}/agent"

# Check if databases exist (containers should have created them)
if [ ! -f "${TESTDATA_DIR}/server/server.db" ]; then
    echo "ERROR: Server database not found at ${TESTDATA_DIR}/server/server.db"
    echo "Make sure containers have started and created their databases."
    exit 1
fi

if [ ! -f "${TESTDATA_DIR}/agent/agent.db" ]; then
    echo "ERROR: Agent database not found at ${TESTDATA_DIR}/agent/agent.db"
    echo "Make sure containers have started and created their databases."
    exit 1
fi

# Seed server data (data-only, no schema)
echo "Seeding server test data..."
sqlite3 "${TESTDATA_DIR}/server/server.db" < "${SEED_DIR}/server_data.sql"

# Seed agent data (data-only, no schema)
echo "Seeding agent test data..."
sqlite3 "${TESTDATA_DIR}/agent/agent.db" < "${SEED_DIR}/agent_data.sql"

# Verify data was seeded
echo ""
echo "Verifying seeded data..."

SERVER_DEVICES=$(sqlite3 "${TESTDATA_DIR}/server/server.db" "SELECT COUNT(*) FROM devices;")
SERVER_AGENTS=$(sqlite3 "${TESTDATA_DIR}/server/server.db" "SELECT COUNT(*) FROM agents;")
AGENT_DEVICES=$(sqlite3 "${TESTDATA_DIR}/agent/agent.db" "SELECT COUNT(*) FROM devices;")

echo "  Server: ${SERVER_DEVICES} devices, ${SERVER_AGENTS} agents"
echo "  Agent:  ${AGENT_DEVICES} devices"

echo ""
echo "=== Seed complete! ==="
