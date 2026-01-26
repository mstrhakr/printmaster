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

# Seed test databases
echo "Seeding test databases..."
./seed-testdata.sh
echo ""

# Build and start containers
echo "Starting Docker containers..."
docker compose -f docker-compose.e2e.yml up -d $BUILD server agent

# Wait for services
echo ""
echo "Waiting for services to be healthy..."

SERVER_READY=false
AGENT_READY=false

for i in {1..30}; do
    if [ "$SERVER_READY" = false ]; then
        if curl -sf http://localhost:8443/api/health > /dev/null 2>&1; then
            echo "  Server is ready!"
            SERVER_READY=true
        elif [ "$VERBOSE" = true ]; then
            echo "  Attempt $i: Server not ready yet..."
        fi
    fi
    
    if [ "$AGENT_READY" = false ]; then
        if curl -sf http://localhost:8080/api/health > /dev/null 2>&1; then
            echo "  Agent is ready!"
            AGENT_READY=true
        elif [ "$VERBOSE" = true ]; then
            echo "  Attempt $i: Agent not ready yet..."
        fi
    fi
    
    if [ "$SERVER_READY" = true ] && [ "$AGENT_READY" = true ]; then
        break
    fi
    
    sleep 2
done

if [ "$SERVER_READY" = false ] || [ "$AGENT_READY" = false ]; then
    echo "Services did not become healthy in time!"
    docker compose -f docker-compose.e2e.yml logs
    exit 1
fi

# Run E2E tests
echo ""
echo "Running E2E tests..."
echo ""

export E2E_SERVER_URL="http://localhost:8443"
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
    trap - EXIT  # Remove cleanup trap
fi

exit $TEST_EXIT_CODE
