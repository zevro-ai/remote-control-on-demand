#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$ROOT_DIR/.rcodbot"
PID_FILE="$STATE_DIR/rcodbot.pid"
LOG_FILE="$STATE_DIR/rcodbot.log"

if [[ ! -f "$PID_FILE" ]]; then
  echo "rcodbot is not running"
  [[ -f "$LOG_FILE" ]] && echo "last log: $LOG_FILE"
  exit 1
fi

PID="$(cat "$PID_FILE")"
if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
  echo "rcodbot is running (pid $PID)"
  echo "log: $LOG_FILE"
  exit 0
fi

echo "stale pid file found; rcodbot is not running"
echo "log: $LOG_FILE"
exit 1
