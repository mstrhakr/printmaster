# Development: test, build, run

This project includes a small PowerShell helper script to run tests, build the agent, start it, and open the UI in your browser.

## Quick start (Windows PowerShell)

Open a PowerShell terminal (pwsh) and run:

```powershell
# from project root (where dev/ exists)
pwsh -NoProfile -ExecutionPolicy Bypass .\dev\launch.ps1
```

This script will:
- Run `go test ./...`. If tests fail, the script exits with non-zero status and does not launch.
- `go build` the agent into `./bin/printmaster-agent.exe`.
- Start the built binary.
- Open the default browser to `http://localhost:8080` and attempt to bring an existing browser window to the front (best-effort).

## Manual commands

If you prefer to run the steps manually:

```powershell
# run tests
go test ./...

# build (from project root)
go build -o ./bin/printmaster-agent.exe ./agent

# run the agent
./bin/printmaster-agent.exe

# open UI
Start-Process 'http://localhost:8080'
```

## Notes
- The script is intentionally conservative: tests must pass before ``go build`` and server start.
- Bringing a browser window to the front is best-effort and depends on which browser is running (the script attempts common browsers).
- For CI, run `go test ./...` and `go build` as separate steps; we can add a CI job or pre-commit hook to enforce this.

## Testing on Linux (WSL)

For cross-platform validation, you can test on Linux using WSL (Windows Subsystem for Linux):

### Install Go in WSL (one-time setup)

```bash
# Download and install Go 1.23.3
wget https://go.dev/dl/go1.23.3.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.3.linux-amd64.tar.gz
rm go1.23.3.linux-amd64.tar.gz

# Add to PATH (append to ~/.bashrc)
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify installation
go version
```

### Run tests on Linux

```bash
# Navigate to agent directory (WSL can access Windows drives at /mnt/c/)
cd /mnt/c/temp/printmaster/agent

# Run all tests
go test -v ./...

# Run specific package tests
go test -v ./storage/...
```

### Notes on cross-platform storage

The storage package uses platform-specific paths:
- **Windows**: `%LOCALAPPDATA%\PrintMaster\devices.db` (e.g., `C:\Users\username\AppData\Local\PrintMaster\devices.db`)
- **Linux**: `~/.local/share/PrintMaster/devices.db` (e.g., `/home/username/.local/share/PrintMaster/devices.db`)
- **macOS**: `~/Library/Application Support/PrintMaster/devices.db`

All tests pass on both Windows and Linux platforms, confirming full cross-platform compatibility.
