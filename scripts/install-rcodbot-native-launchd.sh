#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$ROOT_DIR/.rcodbot"
LOG_DIR="$STATE_DIR/launchd"
BIN_PATH="$ROOT_DIR/rcodbot"
CONFIG_PATH="${1:-$ROOT_DIR/config.yaml}"
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"
TEMPLATE_PATH="$ROOT_DIR/packaging/launchd/ai.zevro.rcodbot.plist"
LABEL="ai.zevro.rcodbot"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST_PATH="$PLIST_DIR/$LABEL.plist"
LAUNCHD_TARGET="gui/$UID/$LABEL"

canonicalize_path() {
  local input="$1"
  local dir
  dir="$(cd -- "$(dirname -- "$input")" && pwd -P)"
  printf '%s/%s\n' "$dir" "$(basename -- "$input")"
}

escape_sed_replacement() {
  printf '%s' "$1" | sed 's/[\/&]/\\&/g'
}

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "config not found: $CONFIG_PATH" >&2
  exit 1
fi

if [[ ! -f "$TEMPLATE_PATH" ]]; then
  echo "launchd template not found: $TEMPLATE_PATH" >&2
  exit 1
fi

CONFIG_PATH="$(canonicalize_path "$CONFIG_PATH")"

mkdir -p "$STATE_DIR" "$LOG_DIR" "$PLIST_DIR"

echo "building rcodbot..."
env \
  GOPROXY="${GOPROXY:-off}" \
  GONOSUMDB="${GONOSUMDB:-*}" \
  GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}" \
  GOMODCACHE="${GOMODCACHE:-$HOME/go/pkg/mod}" \
  GOTMPDIR="${GOTMPDIR:-$ROOT_DIR/.gotmp}" \
  "$GO_BIN" build -o "$BIN_PATH" ./cmd/rcodbot

echo "writing launchd agent to $PLIST_PATH..."
sed \
  -e "s/<REPO_PATH>/$(escape_sed_replacement "$ROOT_DIR")/g" \
  -e "s/<CONFIG_PATH>/$(escape_sed_replacement "$CONFIG_PATH")/g" \
  -e "s/<LOG_PATH>/$(escape_sed_replacement "$LOG_DIR")/g" \
  "$TEMPLATE_PATH" >"$PLIST_PATH"

echo "reloading launchd agent..."
launchctl bootout "$LAUNCHD_TARGET" >/dev/null 2>&1 || true

if ! launchctl bootstrap "gui/$UID" "$PLIST_PATH"; then
  echo "bootstrap failed, retrying once..."
  sleep 1
  launchctl bootstrap "gui/$UID" "$PLIST_PATH"
fi

launchctl enable "$LAUNCHD_TARGET" >/dev/null 2>&1 || true
launchctl kickstart -k "$LAUNCHD_TARGET"

echo "launchd agent installed"
echo "label: $LABEL"
echo "plist: $PLIST_PATH"
echo "stdout log: $LOG_DIR/rcodbot.stdout.log"
echo "stderr log: $LOG_DIR/rcodbot.stderr.log"
