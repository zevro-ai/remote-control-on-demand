#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$ROOT_DIR/.codexbot"
PID_FILE="$STATE_DIR/codexbot.pid"
LOG_FILE="$STATE_DIR/codexbot.log"

if [[ ! -f "$PID_FILE" ]]; then
  echo "codexbot is not running"
  [[ -f "$LOG_FILE" ]] && echo "last log: $LOG_FILE"
  exit 1
fi

PID="$(cat "$PID_FILE")"
if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
  echo "codexbot is running (pid $PID)"
  echo "log: $LOG_FILE"
  exit 0
fi

echo "stale pid file found; codexbot is not running"
echo "log: $LOG_FILE"
exit 1
