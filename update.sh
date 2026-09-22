#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKIP_BUILD=0
QUIET=0
LOG_PATH="$SCRIPT_DIR/update.log"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-build)
      SKIP_BUILD=1
      ;;
    --quiet)
      QUIET=1
      ;;
    -h|--help)
      printf 'Usage: ./update.sh [--skip-build] [--quiet]\n'
      exit 0
      ;;
  esac
  shift
done

log() {
  printf '[%s] %s\n' "$(date +'%Y-%m-%dT%H:%M:%S%z')" "$1" | tee -a "$LOG_PATH" >/dev/null
}

warn() {
  printf '[%s] WARN: %s\n' "$(date +'%Y-%m-%dT%H:%M:%S%z')" "$1" | tee -a "$LOG_PATH" >/dev/null
}

err() {
  printf '[%s] ERROR: %s\n' "$(date +'%Y-%m-%dT%H:%M:%S%z')" "$1" | tee -a "$LOG_PATH" >/dev/null
}

run_cmd() {
  log "Running: $*"
  if [[ "$QUIET" == "1" ]]; then
    "$@" >/dev/null 2>&1 || return 1
  else
    "$@" 2>&1 | tee -a "$LOG_PATH"
  fi
}

if [[ $EUID -eq 0 ]]; then
  sudo_cmd=()
elif command -v sudo >/dev/null 2>&1; then
  sudo_cmd=(sudo)
else
  err "Root privileges are required to update /usr/local/bin, and sudo is unavailable"
  exit 1
fi

run_privileged() {
  "${sudo_cmd[@]}" "$@"
}

stop_services() {
  if command -v systemctl >/dev/null 2>&1; then
    run_privileged systemctl stop printmaster-server.service >/dev/null 2>&1 || true
    run_privileged systemctl stop printmaster-agent.service >/dev/null 2>&1 || true
  fi
  pkill -f 'printmaster-server' >/dev/null 2>&1 || true
  pkill -f 'printmaster-agent' >/dev/null 2>&1 || true
}

copy_binary() {
  local src="$1" dst="$2"
  if [[ ! -f "$src" ]]; then
    err "Source binary not found: $src"
    exit 1
  fi
  if [[ -f "$dst" ]]; then
    local bak
    bak="$dst.$(date +'%Y%m%d-%H%M%S').bak"
    log "Backing up $dst -> $bak"
    run_privileged mv "$dst" "$bak" || warn "Backup failed for $dst"
  fi
  log "Copying $src -> $dst"
  run_privileged install -m 755 "$src" "$dst"
}

: > "$LOG_PATH"
log "Update script starting. SkipBuild=$SKIP_BUILD"

stop_services

if [[ "$SKIP_BUILD" == "0" ]]; then
  log "Running build script: ./build.sh both"
  export PRINTMASTER_SKIP_TESTS=1
  if ! run_cmd "$SCRIPT_DIR/build.sh" both; then
    err "Build failed - aborting update"
    exit 1
  fi
  unset PRINTMASTER_SKIP_TESTS
fi

install_dir="/usr/local/bin"

if [[ ! -d "$install_dir" ]]; then
  run_privileged mkdir -p "$install_dir"
fi

copy_binary "$SCRIPT_DIR/agent/printmaster-agent" "$install_dir/printmaster-agent"
copy_binary "$SCRIPT_DIR/server/printmaster-server" "$install_dir/printmaster-server"

if command -v systemctl >/dev/null 2>&1; then
  run_cmd run_privileged systemctl start printmaster-server.service || true
  run_cmd run_privileged systemctl start printmaster-agent.service || true
fi

log "Update script completed successfully"
