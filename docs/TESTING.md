# Testing and CI guidelines

This document describes how to write fast, deterministic tests for the PrintMaster agent.

Why mocking is necessary
-----------------------

Network operations (ICMP, TCP connect, SNMP Get/Walk) are slow and flaky in CI. Unit tests must avoid touching the real network so they remain fast and reliable. The project uses small package-level factories and interfaces to make mocking easy.

Key patterns
------------

- SNMPClient interface: production code uses `NewSNMPClient` (returns a `SNMPClient`) which wraps `gosnmp`. Tests replace `NewSNMPClient` with a mock or fake implementation.
- DoPing: package-level variable that points to the real `pingWithExec` by default. Tests override `DoPing` to return deterministic ping results.
- Use short context timeouts in tests (1–5s) to guard against runaway scans.
- Avoid calling `DiscoverPrinters()` in unit tests; prefer `ScanRangesWithWorkers` or `DiscoverPrintersInRanges` with injected mocks.

Example test pattern
--------------------

```go
oldNew := agent.NewSNMPClient
agent.NewSNMPClient = func(cfg *agent.SNMPConfig, target string, timeout int) (agent.SNMPClient, error) {
    return &agenttest.MockSNMP{ /* preset varbinds */ }, nil
}
defer func(){ agent.NewSNMPClient = oldNew }()

oldPing := agent.DoPing
agent.DoPing = func(ip string, logFn func(string)) bool { return true }
defer func(){ agent.DoPing = oldPing }()

ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
printers, err := agent.ScanRangesWithWorkers(ctx, nil, []string{"127.0.0.1"}, 1, nil, 1)
// assert on printers and behavior
```

Mocking tips
------------

- Keep mocks simple and stateless where possible.
- To simulate timeouts or retries, have the mock delay the response or return an error on the first N calls.
- For concurrent tests, ensure the mock implementations are goroutine-safe (use channels or mutexes if necessary).

Integration testing
-------------------

For full-network tests that exercise the real SNMP stack, maintain separate manual integration scripts (not executed by default in CI). Put those in `scripts/integration/` and document the required environment and credentials.


