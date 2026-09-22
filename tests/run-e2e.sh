#!/bin/bash
# Run E2E Docker tests locally
# This script sets up the Docker environment, runs tests, and cleans up.
#
# Usage: ./run-e2e.sh [options]
#   --build     Force rebuild containers
#   --keep-up   Don't stop containers after tests
#   --verbose   Show verbose output
#
# Example:
#   ./run-e2e.sh
#   ./run-e2e.sh --build --verbose

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD=""
KEEP_UP=false
VERBOSE=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --build)
            BUILD="--build"
            shift
            ;;
        --keep-up)
            KEEP_UP=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo "=== PrintMaster E2E Docker Tests ==="
echo ""

cd "$SCRIPT_DIR"

# Cleanup function
cleanup() {
    if [ "$KEEP_UP" = false ]; then
        echo ""
        echo "Stopping containers..."
        docker compose -f docker-compose.e2e.yml down -v 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Build and start containers
echo "Starting Docker containers..."
docker compose -f docker-compose.e2e.yml up -d $BUILD server agent

wait_for_services() {
    local server_ready=false agent_ready=false
    echo ""
    echo "Waiting for services to be healthy..."

    for i in {1..30}; do
        if [ "$server_ready" = false ] && curl -sf http://localhost:9090/health > /dev/null 2>&1; then
            echo "  Server is ready!"
            server_ready=true
        fi
        if [ "$agent_ready" = false ] && curl -sf http://localhost:8080/health > /dev/null 2>&1; then
            echo "  Agent is ready!"
            agent_ready=true
        fi
        if [ "$server_ready" = true ] && [ "$agent_ready" = true ]; then
            return 0
        fi
        if [ "$VERBOSE" = true ]; then
            echo "  Attempt $i: waiting for services..."
        fi
        sleep 2
    done

    echo "Services did not become healthy in time!"
    docker compose -f docker-compose.e2e.yml logs
    return 1
}

wait_for_services

# Seed only after the applications create their schemas, with containers stopped
# so sqlite3 does not race the applications' WAL connections.
echo ""
echo "Stopping containers before seeding test databases..."
docker compose -f docker-compose.e2e.yml down
./seed-testdata.sh

echo ""
echo "Restarting Docker containers with seeded databases..."
docker compose -f docker-compose.e2e.yml up -d server agent
wait_for_services

# Run E2E tests
echo ""
echo "Running E2E tests..."
echo ""

export E2E_SERVER_URL="http://localhost:9090"
export E2E_AGENT_URL="http://localhost:8080"
export E2E_ADMIN_PASSWORD="e2e-test-password"

set +e
go test -tags=e2e -v -count=1 ./...
TEST_EXIT_CODE=$?
set -e

# Show container status
if [ "$VERBOSE" = true ]; then
    echo ""
    echo "Container status:"
    docker compose -f docker-compose.e2e.yml ps
fi

# Final status
echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "=== E2E Tests PASSED ==="
else
    echo "=== E2E Tests FAILED ==="
fi

if [ "$KEEP_UP" = true ]; then
    echo ""
    echo "Containers left running. Stop with:"
    echo "  docker compose -f tests/docker-compose.e2e.yml down -v"
fi

cleanup
trap - EXIT
exit $TEST_EXIT_CODE
