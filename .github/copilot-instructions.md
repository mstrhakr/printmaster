# Copilot Instructions for PrintMaster Project

## Cross-Platform Support

- Full cross-platform compatibility (Windows, Mac, Linux) is paramount for project success. All code, dependencies, and features must be designed, tested, and maintained to work seamlessly across supported operating systems.

## Code Organization & Best Practices

- Prioritize clean, professional, and maintainable code
- Modular code is good, Smaller file sizes where appropriate, if you think you should pull out a module you should ask about it. keep main down in size when you can/should
- Follow industry standards and best practices for Go and UI development
- Organize code by feature and responsibility (e.g., agent logic, UI, utilities)

## Testing Guidelines

- Write tests for all major features and maintain high coverage
- Use `t.Parallel()` in tests to run them concurrently when possible
- For table-driven tests or tests with multiple subtests, add `t.Parallel()` to both the parent test and each subtest
- Parallel tests significantly reduce test execution time (e.g., 24s → 6s for agent tests)
- Ensure tests are independent and don't share mutable state when running in parallel

### Running Tests

- **ALWAYS use the `runTests` tool** instead of terminal commands for running tests
- If tests pass or fail, the code already built successfully (compilation verified)
- Use `runTests` with specific file paths to run targeted tests
- Only use terminal for non-test commands (benchmarks, manual builds, etc.)
- The `runTests` tool is pre-approved and won't prompt for user confirmation

## UI Architecture

- Desktop UI should only be implemented if it is easy and fully cross-platform (Windows, Mac, Linux). Prioritize web UI for universal compatibility.
- Avoid tightly coupling UI and agent logic
- Use interfaces and dependency injection for testability
- Document all exported functions, types, and modules
- Use consistent formatting (`gofmt`) and linting (`golint`)
- Maintain up-to-date documentation and roadmap files
- Scaffold tests for all major features and keep coverage high

## Project Structure

## Data Storage Plan

- Use in-memory storage (Go slice/map) for discovered printer info before uploading to server
- Prioritize lightweight, fast, and dependency-free agent operation
- Consider persistent storage (SQLite/BoltDB) only if needed for reliability or advanced features
- Utilities and helpers in a separate package

## Tool Usage Priority

- **ALWAYS prefer built-in tools over terminal commands**
- Use `runTests` for running Go tests (pre-approved, no user prompt)
- Use `read_file`, `grep_search`, `semantic_search` for code exploration
- Use `replace_string_in_file`, `create_file` for code changes
- Use `get_errors` to check for compilation errors
- Only resort to terminal for:
  - Benchmarks (`go test -bench`)
  - Manual builds when not triggered by tests
  - Non-Go commands (git, npm, etc.)
  - Background processes (servers, watch mode)

## Terminal Usage Guidelines

- **ALWAYS check the terminal's current working directory before running commands**
- Use the terminal context provided to know where you are (Cwd field)
- **CRITICAL: Use ONLY absolute paths with `cd` command - NEVER use relative paths**
  - ✅ Correct: `cd C:\temp\printmaster\agent`
  - ❌ Wrong: `cd agent` or `cd ..\agent` or `cd .\storage`
- If you need to change directories:
  1. Check current Cwd from terminal context
  2. Determine the full absolute path needed
  3. Use `cd <absolute-path>` with complete path
- Never assume you're in a specific directory - verify first by checking Cwd
- Example: If Cwd is `C:\temp\printmaster\agent` and you need storage subdirectory, use `cd C:\temp\printmaster\agent\storage` (NOT `cd storage`)

## Build and Compilation

- **If tests pass or fail, compilation was already successful**
- Don't run separate `go build` commands after successful test runs
- Use `get_errors` tool to check for compile-time errors before running tests
- Test execution implies successful build of the tested package

## Project Structure Reference

```
c:\temp\printmaster\
├── agent\                      # Main Go module (go.mod here)
│   ├── main.go                 # HTTP server, embedded UI
│   ├── agent\                  # Core discovery logic
│   ├── storage\               # Database layer (SQLite)
│   ├── logger\                # Structured logging
│   ├── scanner\               # Network scanning
│   ├── util\                  # Helpers
│   └── tools\                 # CLI utilities
├── .github\
│   └── copilot-instructions.md # This file
├── docs\                      # Documentation
├── .vscode\
│   └── tasks.json             # Build tasks
└── logs\                      # Runtime logs
```

## Key Database Schema

### storage.Device
- 26 fields: Serial (PK), IP, Manufacturer, Model, Hostname, Firmware, MACAddress, etc.
- NO PageCount, NO TonerLevels (removed in schema v7 - moved to metrics_history)
- IsSaved bool, Visible bool for filtering

### storage.MetricsSnapshot
- Timestamp, Serial, PageCount, ColorPages, MonoPages, ScanCount
- TonerLevels map[string]interface{}
- Time-series data separated from device identity in metrics_history table

## Contribution Guidelines

- All code should be clean, well-documented, and follow project structure
- New features should include tests and documentation
- Roadmap and structure files should be updated with major changes

---

These instructions ensure the project remains organized, professional, and easy to maintain as it grows.
