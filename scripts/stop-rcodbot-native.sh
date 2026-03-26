#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$ROOT_DIR/.rcodbot"
PID_FILE="$STATE_DIR/rcodbot.pid"

if [[ ! -f "$PID_FILE" ]]; then
  echo "rcodbot is not running"
  exit 0
fi

PID="$(cat "$PID_FILE")"
if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
  kill "$PID"
  for _ in {1..10}; do
    if ! kill -0 "$PID" 2>/dev/null; then
      rm -f "$PID_FILE"
      echo "rcodbot stopped"
      exit 0
    fi
    sleep 1
  done

  echo "rcodbot did not exit after 10s; send SIGKILL manually if needed" >&2
  exit 1
fi

rm -f "$PID_FILE"
echo "stale pid file removed"
