#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$ROOT_DIR/.codexbot"
PID_FILE="$STATE_DIR/codexbot.pid"
LOG_FILE="$STATE_DIR/codexbot.log"
BIN_PATH="$ROOT_DIR/codexbot"
CONFIG_PATH="${1:-$ROOT_DIR/config.yaml}"
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"

canonicalize_path() {
  local input="$1"
  local dir
  dir="$(cd -- "$(dirname -- "$input")" && pwd -P)"
  printf '%s/%s\n' "$dir" "$(basename -- "$input")"
}

find_existing_pids() {
  local ps_output
  ps_output="$(ps -wwaxo pid=,command= 2>/dev/null || true)"
  if [[ -z "$ps_output" ]]; then
    return 0
  fi

  awk -v target="$BIN_PATH -config $CONFIG_PATH" '
    {
      pid = $1
      $1 = ""
      sub(/^ +/, "", $0)
      if ($0 == target) {
        print pid
      }
    }
  ' <<<"$ps_output"
}

stop_pid() {
  local pid="$1"
  local label="$2"

  if [[ -z "$pid" ]] || ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi

  echo "stopping $label (pid $pid)..."
  kill "$pid"
  for _ in {1..10}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done

  echo "$label did not exit after 10s; sending SIGKILL..."
  kill -9 "$pid" 2>/dev/null || true
  for _ in {1..5}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done

  echo "failed to stop $label (pid $pid)" >&2
  return 1
}

stop_existing_instances() {
  local pidfile_pid=""
  local found_running=false
  local pid

  if [[ -f "$PID_FILE" ]]; then
    pidfile_pid="$(cat "$PID_FILE")"
    if [[ -n "$pidfile_pid" ]]; then
      stop_pid "$pidfile_pid" "pidfile codexbot" || return 1
    fi
    rm -f "$PID_FILE"
  fi

  while IFS= read -r pid; do
    [[ -z "$pid" ]] && continue
    if [[ -n "$pidfile_pid" && "$pid" == "$pidfile_pid" ]]; then
      continue
    fi
    found_running=true
    stop_pid "$pid" "existing codexbot for $CONFIG_PATH" || return 1
  done < <(find_existing_pids)

  if [[ "$found_running" == true ]]; then
    echo "previous codexbot instances stopped"
  fi
}

mkdir -p "$STATE_DIR"

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "config not found: $CONFIG_PATH" >&2
  exit 1
fi

CONFIG_PATH="$(canonicalize_path "$CONFIG_PATH")"

echo "building codexbot..."
env \
  GOPROXY="${GOPROXY:-off}" \
  GONOSUMDB="${GONOSUMDB:-*}" \
  GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}" \
  GOMODCACHE="${GOMODCACHE:-$HOME/go/pkg/mod}" \
  GOTMPDIR="${GOTMPDIR:-$ROOT_DIR/.gotmp}" \
  "$GO_BIN" build -o "$BIN_PATH" ./cmd/codexbot

stop_existing_instances

echo "starting codexbot..."
(
  cd "$ROOT_DIR"
  nohup "$BIN_PATH" -config "$CONFIG_PATH" >>"$LOG_FILE" 2>&1 &
  echo "$!" >"$PID_FILE"
)

sleep 2

STARTED_PID="$(cat "$PID_FILE")"
if kill -0 "$STARTED_PID" 2>/dev/null; then
  echo "codexbot started (pid $STARTED_PID)"
  echo "log: $LOG_FILE"
  exit 0
fi

echo "codexbot failed to stay up; see $LOG_FILE" >&2
exit 1
