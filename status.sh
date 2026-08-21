#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

section() {
  printf '\n╔══════════════════════════════════════════════════════╗\n'
  printf '║  %-50s  ║\n' "$1"
  printf '╚══════════════════════════════════════════════════════╝\n'
}

item() {
  printf '  %-20s: %s\n' "$1" "$2"
}

section "Version Information"
agent_version="$([[ -f "$SCRIPT_DIR/agent/VERSION" ]] && tr -d '\r\n' < "$SCRIPT_DIR/agent/VERSION" || echo 'NOT FOUND')"
server_version="$([[ -f "$SCRIPT_DIR/server/VERSION" ]] && tr -d '\r\n' < "$SCRIPT_DIR/server/VERSION" || echo 'NOT FOUND')"
item "Agent Version" "$agent_version"
item "Server Version" "$server_version"

section "Git Status"
git_branch="$(git -C "$SCRIPT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
git_commit="$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || true)"
git_remote="$(git -C "$SCRIPT_DIR" remote get-url origin 2>/dev/null || true)"
git_status="$(git -C "$SCRIPT_DIR" status --porcelain 2>/dev/null || true)"

if [[ -n "$git_branch" ]]; then
  item "Branch" "$git_branch"
  item "Commit" "$git_commit"
  item "Remote" "$git_remote"
  if [[ -n "$git_status" ]]; then
    item "Working Directory" "UNCOMMITTED CHANGES"
    printf '\n%s\n' "$git_status"
  else
    item "Working Directory" "Clean"
  fi
else
  printf '  Not a git repository\n'
fi

if [[ -n "$git_branch" ]]; then
  unpushed="$(git -C "$SCRIPT_DIR" log "origin/$git_branch..$git_branch" --oneline 2>/dev/null || true)"
  if [[ -n "$unpushed" ]]; then
    printf '\n  Unpushed commits:\n%s\n' "$unpushed"
  fi
fi

section "Recent Tags"
recent_tags="$(git -C "$SCRIPT_DIR" tag -l --sort=-version:refname 2>/dev/null | head -n 5 || true)"
if [[ -n "$recent_tags" ]]; then
  printf '%s\n' "$recent_tags"
else
  printf '  No tags found\n'
fi

section "Build Artifacts"
agent_exe="$SCRIPT_DIR/agent/printmaster-agent"
server_exe="$SCRIPT_DIR/server/printmaster-server"
if [[ -f "$agent_exe" ]]; then
  size_mb="$(awk "BEGIN { printf \"%.2f\", $(stat -c %s "$agent_exe") / 1048576 }")"
  item "Agent Binary" "$size_mb MB"
else
  item "Agent Binary" "NOT BUILT"
fi
if [[ -f "$server_exe" ]]; then
  size_mb="$(awk "BEGIN { printf \"%.2f\", $(stat -c %s "$server_exe") / 1048576 }")"
  item "Server Binary" "$size_mb MB"
else
  item "Server Binary" "NOT BUILT"
fi

section "Running Processes"
if command -v pgrep >/dev/null 2>&1; then
  if pgrep -af 'printmaster|debug_bin' >/dev/null 2>&1; then
    pgrep -af 'printmaster|debug_bin'
  else
    printf '  No PrintMaster processes running\n'
  fi
else
  printf '  pgrep not available\n'
fi

section "Quick Actions"
printf '  Build:   ./build.sh agent\n'
printf '  Test:    ./build.sh test-all\n'
printf '  Release: ./release.sh agent patch\n'
printf '  Debug:   Press F5 in VS Code\n'
