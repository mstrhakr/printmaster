# Build & Release Workflow

## Quick Reference

### Daily Development

```powershell
# Build agent for development (with debug info)
.\build.ps1 agent

# Build server
.\build.ps1 server

# Build both
.\build.ps1 both

# Run tests
.\build.ps1 test-all

# Clean artifacts
.\build.ps1 clean
```

### VS Code Tasks (Ctrl+Shift+B)

- **Build: Agent (Dev)** - Default build task
- **Build: Server (Dev)** - Build server
- **Build: Both (Dev)** - Build both components
- **Test: Agent (all)** - Run all agent tests
- **Test: Server (all)** - Run all server tests
- **Show Version** - Display current versions
- **Show Build Log** - View recent build output

### VS Code Debug (F5)

- **Debug: Agent (Default Port)** - Launch agent on port 8080
- **Debug: Agent (Port 9090)** - Launch agent on port 9090
- **Debug: Server (Default Port)** - Launch server on port 3000
- **Debug: Agent + Server Together** - Launch both simultaneously

### Making Releases

```powershell
# Patch release (0.1.0 → 0.1.1) - Bug fixes
.\release.ps1 agent patch

# Minor release (0.1.0 → 0.2.0) - New features, backward compatible
.\release.ps1 agent minor

# Major release (0.1.0 → 1.0.0) - Breaking changes
.\release.ps1 agent major

# Release server
.\release.ps1 server patch

# Release both components together
.\release.ps1 both patch
```

**What `release.ps1` does:**
1. ✅ Checks git status (warns if uncommitted changes)
2. ✅ Bumps version in VERSION file
3. ✅ Runs all tests
4. ✅ Builds release binary (optimized, stripped)
5. ✅ Commits VERSION change
6. ✅ Tags release (e.g., v0.2.0)
7. ✅ Pushes to GitHub

### Release Flags

```powershell
# Dry run (see what would happen without doing it)
.\release.ps1 agent patch -DryRun

# Skip tests (not recommended!)
.\release.ps1 agent patch -SkipTests

# Skip GitHub push (for local testing)
.\release.ps1 agent patch -SkipPush
```

### Git Workflow

```powershell
# Check status
git status

# Stage all changes
git add -A

# Commit
git commit -m "your message"

# Push
git push

# Or use VS Code tasks:
# - Git: Status
# - Git: Commit All
# - Git: Push
# - Git: Pull
```

## Semantic Versioning Guide

Format: `MAJOR.MINOR.PATCH`

### PATCH (0.1.0 → 0.1.1)
- Bug fixes
- Performance improvements
- Documentation updates
- No new features
- **100% backward compatible**

**Examples:**
- Fix SNMP parsing error
- Update vendor OID mapping
- Improve error messages

### MINOR (0.1.0 → 0.2.0)
- New features
- New functionality
- Deprecations (with backward compatibility)
- **Backward compatible** (existing code still works)

**Examples:**
- Add new printer vendor support
- Add metrics export endpoint
- Add configuration option

### MAJOR (0.1.0 → 1.0.0)
- Breaking changes
- Remove deprecated features
- Change API contracts
- **NOT backward compatible**

**Examples:**
- Remove old API endpoints
- Change database schema (non-compatible)
- Change configuration format

## Pre-1.0 Development

During `0.x.x` versions, breaking changes are acceptable in MINOR releases since the API is not yet stable. Once you hit `1.0.0`, you must follow strict SemVer rules.

## Version Strategy

- **Agent**: Independent versioning (VERSION file at root)
- **Server**: Independent versioning (server/VERSION file)
- **Tags**:
  - Agent releases: `v0.2.0`
  - Server releases: `server-v0.2.0`
  - Combined releases: `v0.2.0` (both bumped together)

## CI/CD Integration (Future)

When you add GitHub Actions:

```yaml
# .github/workflows/release.yml
on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v3
      - name: Build Release
        run: .\build.ps1 agent
      - name: Create GitHub Release
        # ... attach binaries
```

## Troubleshooting

### "Uncommitted changes detected"
```powershell
# Commit or stash changes first
git add -A
git commit -m "description"

# Or stash temporarily
git stash
.\release.ps1 agent patch
git stash pop
```

### "Tests failed"
```powershell
# Run tests manually to see details
cd agent
go test ./... -v

# Fix tests, then retry release
```

### "Build failed"
```powershell
# Check build log
Get-Content logs\build.log -Tail 50

# Or use VS Code task: "Show Build Log"
```

### Release went wrong
```powershell
# Undo last commit (keep changes)
git reset HEAD~1

# Restore VERSION file
git restore VERSION

# Delete tag
git tag -d v0.2.0

# Start over
```

## Best Practices

1. **Always commit working code before releasing**
2. **Write meaningful commit messages**
3. **Test locally before pushing**
4. **Use patch for bug fixes, minor for features**
5. **Document breaking changes in CHANGELOG.md**
6. **Tag releases immediately after merge to main**

## Example Workflow

```powershell
# 1. Start feature work
git checkout -b feature/new-scanner

# 2. Make changes, test locally
.\build.ps1 agent
.\build.ps1 test-all

# 3. Commit work
git add -A
git commit -m "feat: Add Ricoh network scanner support"

# 4. Merge to main
git checkout main
git merge feature/new-scanner

# 5. Release (minor version - new feature)
.\release.ps1 agent minor

# Done! Version bumped, tagged, and pushed to GitHub
```

## VS Code Integration

All build, test, and release commands are available via:
- **Command Palette** (Ctrl+Shift+P): "Tasks: Run Task"
- **Keyboard Shortcuts**:
  - `Ctrl+Shift+B` - Build menu
  - `F5` - Start debugging
  - `Shift+F5` - Stop debugging
- **Tasks Explorer** (Terminal → Run Task)
