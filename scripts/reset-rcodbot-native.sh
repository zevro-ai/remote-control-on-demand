#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$ROOT_DIR/.rcodbot"
LOG_FILE="$STATE_DIR/rcodbot.log"
CONFIG_PATH="${1:-$ROOT_DIR/config.yaml}"
ROTATE_LOG="${ROTATE_LOG:-1}"

mkdir -p "$STATE_DIR"

if [[ "${ROTATE_LOG}" == "1" && -f "$LOG_FILE" ]]; then
  timestamp="$(date +%Y%m%d-%H%M%S)"
  rotated_log="$STATE_DIR/rcodbot.$timestamp.log"
  mv "$LOG_FILE" "$rotated_log"
  echo "rotated log to $rotated_log"
fi

echo "stopping rcodbot if it is running..."
"$ROOT_DIR/scripts/stop-rcodbot-native.sh" || true

echo "starting rcodbot..."
"$ROOT_DIR/scripts/start-rcodbot-native.sh" "$CONFIG_PATH"

echo
"$ROOT_DIR/scripts/status-rcodbot-native.sh"
echo "tail logs with: tail -f $LOG_FILE"
