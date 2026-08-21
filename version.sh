#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

agent_version_file="$SCRIPT_DIR/agent/VERSION"
server_version_file="$SCRIPT_DIR/server/VERSION"

if [[ -f "$agent_version_file" ]]; then
  printf 'Agent Version:   %s\n' "$(tr -d '\r\n' < "$agent_version_file")"
else
  printf 'agent/VERSION file not found!\n' >&2
fi

if [[ -f "$server_version_file" ]]; then
  printf 'Server Version:  %s\n' "$(tr -d '\r\n' < "$server_version_file")"
else
  printf 'server/VERSION file not found!\n' >&2
fi

if git -C "$SCRIPT_DIR" rev-parse --short HEAD >/dev/null 2>&1; then
  printf 'Git Commit:      %s\n' "$(git -C "$SCRIPT_DIR" rev-parse --short HEAD)"
  printf 'Git Branch:      %s\n' "$(git -C "$SCRIPT_DIR" rev-parse --abbrev-ref HEAD)"
fi

agent_binary="$SCRIPT_DIR/agent/printmaster-agent"
agent_windows_binary="$SCRIPT_DIR/agent/printmaster-agent.exe"
if [[ -f "$agent_binary" || -f "$agent_windows_binary" ]]; then
  binary_path="$agent_binary"
  [[ -f "$binary_path" ]] || binary_path="$agent_windows_binary"
  printf '\nBuilt Binary:\n'
  printf '  Built:         %s\n' "$(stat -c '%y' "$binary_path")"
  printf '  Size:          %s MB\n' "$(awk 'BEGIN { printf "%.2f", '$(( $(stat -c %s "$binary_path") ))' / 1048576 }')"
fi
