#!/usr/bin/env bash
# One-liner friendly bootstrap for cwbackup-tui
# Usage:
#   bash <(curl -fsSL https://raw.githubusercontent.com/sameerakhtari/cw-scripts-tui/refs/heads/main/bootstrap/domain-based-backup-tui.sh)
# Optional:
#   REL=v0.1.0 CW_EMAIL=... CW_API_KEY=... CW_DOMAINS=$'a.com\nb.com' bash <(curl -fsSL ...)

set -euo pipefail

REL="${REL:-v0.1.0}"  # Tag in this repo's Releases; change once you publish the first release.

# Where to fetch binaries from
REPO_DL="https://github.com/sameerakhtari/cw-scripts-tui/releases/download"

# Where to fetch the original bash backup script from
SCRIPT_RAW="https://raw.githubusercontent.com/sameerakhtari/CW-Scripts/refs/heads/main/domain-based-backup.sh"

# curl or wget
fetch() {
  local url="$1" out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 -o "$out" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    echo "Neither curl nor wget found." >&2
    return 1
  fi
}

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"   # linux/darwin
ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported arch: $ARCH_RAW"
    echo "Falling back to non-TUI..."
    fetch "$SCRIPT_RAW" /tmp/domain-based-backup.sh
    chmod +x /tmp/domain-based-backup.sh
    /bin/bash /tmp/domain-based-backup.sh
    exit 0
  ;;
esac

BIN_URL="$REPO_DL/$REL/cwbackup-tui_${OS}_${ARCH}"
BIN="/tmp/cwbackup-tui"
SCRIPT="/tmp/domain-based-backup.sh"

echo "[*] Downloading TUI: $BIN_URL"
if ! fetch "$BIN_URL" "$BIN"; then
  echo "TUI download failed; falling back to non-TUI..."
  fetch "$SCRIPT_RAW" "$SCRIPT"
  chmod +x "$SCRIPT"
  /bin/bash "$SCRIPT"
  exit 0
fi
chmod +x "$BIN"

echo "[*] Fetching backup script..."
fetch "$SCRIPT_RAW" "$SCRIPT"
chmod +x "$SCRIPT"

echo "[*] Launching TUI..."
CW_BACKUP_SCRIPT="$SCRIPT" "$BIN"
