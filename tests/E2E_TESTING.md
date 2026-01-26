# E2E Testing Strategy

## Overview

End-to-end tests verify that the agent and server work together correctly. These tests are more expensive than unit tests but provide confidence that the full system functions as expected.

## Quick Start

### Run E2E Tests Locally

```bash
# Linux/macOS
cd tests
./run-e2e.sh

# Windows
cd tests
.\run-e2e.ps1
```

### Options

| Option | Description |
|--------|-------------|
| `--build` / `-Build` | Force rebuild Docker containers |
| `--keep-up` / `-KeepUp` | Leave containers running after tests |
| `--verbose` / `-Verbose` | Show detailed output |

## Architecture

### Docker Compose Environment

The E2E tests use Docker Compose to create an isolated test environment:

```
┌─────────────────────────────────────────────────────────┐
│                 E2E Test Network                        │
│                                                         │
│  ┌─────────────┐         ┌─────────────┐              │
│  │   Server    │◀───────▶│    Agent    │              │
│  │  :8443      │   WS    │   :8080     │              │
│  │             │         │             │              │
│  │ SQLite DB   │         │ SQLite DB   │              │
│  └─────────────┘         └─────────────┘              │
│         ▲                       ▲                      │
│         │                       │                      │
│  ┌──────┴──────┐         ┌──────┴──────┐              │
│  │ Seed Data   │         │ Seed Data   │              │
│  │ server.db   │         │ agent.db    │              │
│  └─────────────┘         └─────────────┘              │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
               ┌─────────────────────┐
               │   Test Runner       │
               │   (go test -tags=e2e)│
               └─────────────────────┘
```

### Test Data

Pre-seeded SQLite databases provide predictable test data:

**Server Database:**
- 1 tenant: "E2E Test Tenant"
- 1 registered agent: "e2e-test-agent"
- 5 test devices (HP, Kyocera, Brother, Lexmark, Xerox)
- Sample metrics for each device
- Admin user (password: `e2e-test-password`)

**Agent Database:**
- Fixed agent UUID: `e2e00000-0000-0000-0000-000000000001`
- 5 test devices (matching server)
- Scanner configuration (discovery disabled)
- Server connection settings

### Regenerating Test Data

```bash
# Linux/macOS
./seed-testdata.sh

# Windows
.\seed-testdata.ps1
```

## Test Categories

### 1. Health Checks
- Server `/api/health` endpoint
- Agent `/api/health` endpoint

### 2. Agent Registration
- Agent registers with server on startup
- Agent UUID matches pre-seeded data
- WebSocket connection establishment

### 3. Device APIs
- Server device list includes seeded devices
- Agent device list includes seeded devices
- Device metrics queries

### 4. Integration Flows
- Full agent-server communication
- Device data synchronization
- Error handling

## CI Integration

E2E Docker tests run automatically in GitHub Actions:

```yaml
# .github/workflows/ci.yml
e2e-docker:
  name: E2E (Docker)
  runs-on: ubuntu-latest
  needs: [agent, server]
  steps:
    - uses: actions/checkout@v4
    - name: Seed test databases
      run: ./tests/seed-testdata.sh
    - name: Start E2E environment
      run: docker compose -f tests/docker-compose.e2e.yml up -d --build
    - name: Run E2E tests
      run: go test -tags=e2e -v ./tests/...
```

## File Structure

```
tests/
├── docker-compose.e2e.yml    # Docker Compose for E2E environment
├── e2e_docker_test.go        # E2E tests (build tag: e2e)
├── run-e2e.sh                # Linux/macOS helper script
├── run-e2e.ps1               # Windows helper script
├── seed-testdata.sh          # Database seed script (Linux/macOS)
├── seed-testdata.ps1         # Database seed script (Windows)
└── testdata/
    ├── server/
    │   └── server.db         # Generated server database
    ├── agent/
    │   ├── agent.db          # Generated agent database
    │   └── agent_id          # Fixed agent UUID
    └── seed/
        ├── server_seed.sql   # Server seed SQL
        └── agent_seed.sql    # Agent seed SQL
```

## Troubleshooting

### Containers won't start
```bash
# Check container logs
docker compose -f tests/docker-compose.e2e.yml logs server
docker compose -f tests/docker-compose.e2e.yml logs agent

# Clean up and retry
docker compose -f tests/docker-compose.e2e.yml down -v
./run-e2e.sh --build
```

### Tests fail with connection errors
```bash
# Verify containers are healthy
docker compose -f tests/docker-compose.e2e.yml ps

# Check health endpoints manually
curl http://localhost:8443/api/health
curl http://localhost:8080/api/health
```

### Database schema mismatch
```bash
# Regenerate seed databases
./seed-testdata.sh

# Or rebuild containers to trigger migrations
./run-e2e.sh --build
```

- Error handling

### 3. WebSocket Proxy (New Feature)
- Proxy to agent UI
- Proxy to device UI
- Timeout handling
- Connection loss handling
- Multiple concurrent requests

### 4. Failure Scenarios
- Server restart while agent connected
- Agent restart
- Network interruption
- Invalid authentication
- Database errors

## Running Tests

```bash
# Run all E2E tests
cd tests
go test -v ./...

# Run specific test
go test -v -run TestWebSocketProxy_AgentUI

# Skip E2E tests (fast unit tests only)
cd agent
go test -short ./...
cd ../server
go test -short ./...
```

## CI/CD Integration

```yaml
# .github/workflows/test.yml
- name: Run Unit Tests
  run: |
    cd agent && go test -short ./...
    cd ../server && go test -short ./...

- name: Build Binaries
  run: |
    ./build.ps1 both

- name: Run E2E Tests
  run: |
    cd tests && go test -v -timeout 5m ./...
```

## Next Steps

1. **Implement process management helpers**
   - `startServer()` - Start server on ephemeral port
   - `startAgent()` - Start agent connected to test server
   - `cleanup()` - Ensure processes are killed

2. **Add config generation**
   - Minimal TOML for server
   - Minimal TOML for agent
   - Temp database paths

3. **Implement first working test**
   - TestAgentServerRegistration
   - Verify agent can register and connect

4. **Add WebSocket proxy tests**
   - TestWebSocketProxy_AgentUI
   - TestWebSocketProxy_DeviceUI
   - TestWebSocketProxy_NoConnection

5. **Add to CI/CD**
   - Run after successful build
   - Report test results
   - Save logs on failure

## Performance Considerations

- E2E tests are slow (2-10 seconds each)
- Use `t.Parallel()` where possible
- Don't run E2E tests by default (use `-short` flag)
- Keep E2E test count reasonable (<20 tests)
- Focus on critical paths and new features

## Debugging Failed Tests

When E2E tests fail:

1. **Check logs** - Server and agent log output
2. **Check ports** - Ensure no port conflicts
3. **Check cleanup** - Previous test may have left processes running
4. **Increase timeouts** - CI may be slower than local
5. **Run locally** - Reproduce outside CI environment

## Example: Complete E2E Test

```go
func TestWebSocketProxyComplete(t *testing.T) {
    // 1. Setup
    tmpDir := t.TempDir()
    serverPort := getFreePort(t)
    agentPort := getFreePort(t)
    
    // 2. Start server
    serverCmd := startServer(t, serverPort, tmpDir)
    defer serverCmd.Process.Kill()
    
    // 3. Wait for server ready
    if !waitForServer(t, serverPort, 5*time.Second) {
        t.Fatal("Server failed to start")
    }
    
    // 4. Start agent
    agentCmd := startAgent(t, agentPort, serverPort, tmpDir)
    defer agentCmd.Process.Kill()
    
    // 5. Wait for connection
    if !waitForAgentActive(t, serverPort, "test-agent", 5*time.Second) {
        t.Fatal("Agent failed to connect")
    }
    
    // 6. Test proxy
    resp := makeProxyRequest(t, serverPort, "test-agent", "/")
    defer resp.Body.Close()
    
    // 7. Verify
    if resp.StatusCode != 200 {
        t.Errorf("Expected 200, got %d", resp.StatusCode)
    }
    
    // 8. Cleanup (defers handle this)
}
```

## Resources

- Go testing: https://go.dev/doc/tutorial/add-a-test
- Integration testing: https://go.dev/wiki/TableDrivenTests
- Test fixtures: https://github.com/golang/go/wiki/TestFixtures
