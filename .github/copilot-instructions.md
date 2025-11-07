# Copilot Instructions for PrintMaster Project

# Always check the docs, especially if we are starting a new project. 

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

## Build & Release Workflow

### Daily Development Workflow

**Use the new automation tools for all build and release tasks:**

1. **Building Code**:

   - Use VS Code tasks (Ctrl+Shift+B) → "Build: Agent (Dev)" or "Build: Server (Dev)"
   - Or run: `.\build.ps1 agent` / `.\build.ps1 server` / `.\build.ps1 both`
   - Build script automatically injects version, git commit, build time into binaries
2. **Running Tests**:

   - ALWAYS use `runTests` tool for Go tests (pre-approved, no prompts)
   - Or use VS Code task: "Test: Agent (all)" / "Test: Server (all)"
   - Tests must pass before any release
3. **Debugging**:

   - Use VS Code launch configs (F5)
   - Available: "Debug: Agent (Default Port)", "Debug: Server (Default Port)", "Debug: Agent + Server Together"
   - All configs auto-kill existing processes via preLaunchTask
4. **Checking Status**:

   - Run `.\status.ps1` to see versions, git status, build artifacts, running processes
   - Use before starting work or before making releases

### Making Releases

**IMPORTANT: Use `release.ps1` for all version bumps and releases**

**When to Release:**

- **PATCH** (0.1.0 → 0.1.1): After bug fixes, performance improvements, docs updates
- **MINOR** (0.1.0 → 0.2.0): After adding new features (backward compatible)
- **MAJOR** (0.1.0 → 1.0.0): After breaking changes or API changes (rare pre-1.0)

**How to Release:**

```powershell
# Patch release (bug fixes)
.\release.ps1 agent patch

# Minor release (new features)
.\release.ps1 agent minor

# Major release (breaking changes)
.\release.ps1 agent major

# Release server component
.\release.ps1 server patch

# Release both components together
.\release.ps1 both patch
```

**What `release.ps1` Does Automatically:**

1. ✅ Checks git status (warns if uncommitted changes)
2. ✅ Bumps version in VERSION file(s) (using SemVer)
3. ✅ Runs all tests (fails if any test fails)
4. ✅ Builds optimized release binary (stripped, production-ready)
5. ✅ Commits VERSION file with message like "chore: Release agent v0.2.0"
6. ✅ Creates git tag (e.g., v0.2.0 for agent, server-v0.2.0 for server)
7. ✅ Pushes commit and tags to GitHub

**Release Flags:**

- `--DryRun` - Preview what would happen without doing it
- `--SkipTests` - Skip test execution (NOT recommended!)
- `--SkipPush` - Commit and tag locally but don't push to GitHub

**Release Checklist for Copilot:**

- [ ] Ask user which component (agent/server/both) and bump type (patch/minor/major)
- [ ] Ensure working directory is clean (or warn user)
- [ ] Let `release.ps1` handle the entire workflow
- [ ] DO NOT manually edit VERSION files - let the script do it
- [ ] DO NOT manually commit/tag - let the script do it
- [ ] After successful release, verify with `git log --oneline -1` and `git tag -l`

### Git Workflow Integration

**When making code changes:**

1. Make changes and test locally (`.\build.ps1 agent`, `runTests`)
2. Commit regularly with meaningful messages:
   - `feat:` for new features
   - `fix:` for bug fixes
   - `docs:` for documentation
   - `chore:` for maintenance tasks
   - `refactor:` for code restructuring
3. Push to GitHub: `git push`
4. When ready to release: Use `.\release.ps1` (never manual version bumps)

**Available VS Code Git Tasks:**

- "Git: Status" - Check current status
- "Git: Commit All" - Stage and commit all changes
- "Git: Push" - Push to GitHub
- "Git: Pull" - Pull latest changes

**Copilot Should:**

- Suggest using `release.ps1` when user asks to "bump version" or "make a release"
- Remind user to commit working changes before releasing
- Use git commands directly for normal commits, but ALWAYS use `release.ps1` for releases
- Check `.\status.ps1` output when unclear about project state

### VS Code Integration Reference

**Tasks (Ctrl+Shift+B):**

- Build: Agent/Server/Both (Dev)
- Test: Agent/Server (all)
- Release: Agent/Server/Both Patch/Minor/Major
- Git: Status/Commit/Push/Pull
- Utility: Kill processes, Show logs, Show version

**Launch Configs (F5):**

- Debug: Agent (Default Port) / Agent (Port 9090) / Agent (Custom Config)
- Debug: Server (Default Port) / Server (Port 8080)
- Debug: Current Test Function / All Tests in Package
- Debug: Agent + Server Together (compound config)

**Helper Scripts:**

- `status.ps1` - Quick project overview
- `build.ps1` - Build components (dev or release mode)
- `release.ps1` - Automated release workflow
- `version.ps1` - Show current versions

## Contribution Guidelines

- All code should be clean, well-documented, and follow project structure
- New features should include tests and documentation
- Roadmap and structure files should be updated with major changes
- Use `release.ps1` for all version bumps and releases (never manual)
- Commit often with conventional commit messages (feat:, fix:, docs:, chore:)
- Test locally before pushing (`runTests` tool or VS Code tasks)

## Documentation Reference

- `docs/BUILD_WORKFLOW.md` - Complete build, test, and release guide
- `GIT_INTEGRATION_SUMMARY.md` - Git/GitHub integration reference
- `docs/ROADMAP_TO_1.0.md` - Feature roadmap and milestones

---

These instructions ensure the project remains organized, professional, and easy to maintain as it grows. The automated build and release workflow keeps versioning consistent and reduces human error.
