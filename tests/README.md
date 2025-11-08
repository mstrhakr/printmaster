# End-to-End Tests

This directory contains integration/E2E tests that test agent and server components together.

## Running Tests

```bash
# Run all E2E tests
cd tests
go test -v ./...

# Run with no cache
go test -v -count=1 ./...

# Skip E2E tests (in other directories)
cd ../agent
go test -short ./...
```

## Test Coverage

### WebSocket Proxy Tests (`websocket_proxy_test.go`)

✅ **TestWebSocketProxy_BasicFlow** - Complete proxy request/response flow  
✅ **TestWebSocketProxy_UnreachableTarget** - Error handling for unreachable targets  
✅ **TestWebSocketProxy_MultipleRequests** - Concurrent proxy requests

### HTTP API Tests (`http_api_test.go`)

✅ **TestHTTPAPI_AgentRegistration** - Agent registration endpoint  
✅ **TestHTTPAPI_Heartbeat** - Agent heartbeat with authentication  
✅ **TestHTTPAPI_UnauthorizedAccess** - Token validation  
✅ **TestHTTPAPI_DeviceUpload** - Device batch upload endpoint

## Test Organization

- `websocket_proxy_test.go` - WebSocket proxy functionality tests
- `http_api_test.go` - HTTP API endpoint tests
- `helpers_test.go` - Shared test utilities

## Test Structure

E2E tests use:
- `httptest.NewServer()` to create mock HTTP servers
- `websocket.DefaultDialer` to test WebSocket connections
- `t.Parallel()` to run tests concurrently
- `testing.Short()` to allow skipping with `-short` flag

## Notes

- Tests use ephemeral ports to avoid conflicts
- Tests run in parallel where possible (~1.5s total execution)
- Mock servers simulate agent and server behavior
- Tests verify both success and error scenarios
