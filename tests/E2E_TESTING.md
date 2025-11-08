# E2E Testing Strategy

## Overview

End-to-end tests verify that the agent and server work together correctly. These tests are more expensive than unit tests but provide confidence that the full system functions as expected.

## Current Status

🚧 **Work in Progress** - E2E test infrastructure is set up but tests are not yet fully implemented.

### What Exists

- Test structure in `tests/` directory
- Helper functions for starting server/agent
- Skeleton tests for WebSocket proxy, registration, heartbeat

### What's Needed

1. **Process Management**
   - Start server binary on random port
   - Start agent binary configured to connect to test server
   - Clean shutdown of processes after tests

2. **Configuration Generation**
   - Generate minimal TOML configs for server and agent
   - Use temp directories for databases
   - Configure WebSocket enabled/disabled

3. **Synchronization**
   - Wait for server to be ready (health check)
   - Wait for agent to register and connect
   - Poll for WebSocket connection establishment

## Implementation Options

### Option 1: Start Binaries (More Realistic)

**Pros:**
- Tests actual binaries that users run
- Catches build/packaging issues
- True end-to-end test

**Cons:**
- Requires building binaries first
- Slower test execution
- More complex process management

**Example:**
```go
func startServerBinary(t *testing.T, port int, dbPath string) *exec.Cmd {
    cmd := exec.Command("../server/printmaster-server",
        "--port", strconv.Itoa(port),
        "--db", dbPath)
    cmd.Start()
    return cmd
}
```

### Option 2: Import Packages (Faster)

**Pros:**
- Faster test execution
- Easier debugging
- No binary build requirement

**Cons:**
- Doesn't test actual binary
- May miss packaging issues
- Need to expose main() logic

**Example:**
```go
import (
    serverPkg "printmaster/server"
    agentPkg "printmaster/agent"
)

func startServerInProcess(t *testing.T, config Config) {
    go serverPkg.RunServer(config)
}
```

### Recommendation: Hybrid Approach

- **Fast tests**: Use in-process (Option 2) for rapid development
- **Release tests**: Use binaries (Option 1) in CI/CD before release
- Tag tests appropriately: `-tags=e2e` for binary tests

## Test Categories

### 1. Basic Communication
- Agent registration
- HTTP heartbeat
- WebSocket heartbeat
- Fallback from WebSocket to HTTP

### 2. Data Upload
- Device batch upload
- Metrics batch upload
- Large payloads
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
