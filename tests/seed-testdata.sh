#!/bin/bash
# Seed E2E test databases
# Creates fresh SQLite databases with test data for E2E testing.
#
# Usage: ./seed-testdata.sh
#
# Requirements: sqlite3 must be installed

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTDATA_DIR="${SCRIPT_DIR}/testdata"

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

# Create directories if needed
mkdir -p "${TESTDATA_DIR}/server"
mkdir -p "${TESTDATA_DIR}/agent"

# Remove old databases
echo "Removing old test databases..."
rm -f "${TESTDATA_DIR}/server/server.db"
rm -f "${TESTDATA_DIR}/agent/agent.db"

# Create server database
echo "Creating server test database..."
sqlite3 "${TESTDATA_DIR}/server/server.db" < "${TESTDATA_DIR}/seed/server_seed.sql"

# Create agent database
echo "Creating agent test database..."
sqlite3 "${TESTDATA_DIR}/agent/agent.db" < "${TESTDATA_DIR}/seed/agent_seed.sql"

# Verify databases
echo ""
echo "Verifying databases..."

SERVER_DEVICES=$(sqlite3 "${TESTDATA_DIR}/server/server.db" "SELECT COUNT(*) FROM devices;")
SERVER_AGENTS=$(sqlite3 "${TESTDATA_DIR}/server/server.db" "SELECT COUNT(*) FROM agents;")
AGENT_DEVICES=$(sqlite3 "${TESTDATA_DIR}/agent/agent.db" "SELECT COUNT(*) FROM devices;")

echo "  Server: ${SERVER_DEVICES} devices, ${SERVER_AGENTS} agents"
echo "  Agent:  ${AGENT_DEVICES} devices"

echo ""
echo "=== Seed complete! ==="
echo ""
echo "Test databases created at:"
echo "  ${TESTDATA_DIR}/server/server.db"
echo "  ${TESTDATA_DIR}/agent/agent.db"
echo ""
echo "Run E2E tests with:"
echo "  docker compose -f tests/docker-compose.e2e.yml up --build"
