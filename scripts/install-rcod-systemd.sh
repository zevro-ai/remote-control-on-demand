#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="system"
SERVICE_USER="rcod"
SERVICE_GROUP=""
CONFIG_PATH=""
STATE_DIR=""
BIN_PATH=""
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"
GO_CACHE_DIR="${GOCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/rcod/go-build}"
GO_MOD_CACHE="${GOMODCACHE:-$HOME/go/pkg/mod}"
GO_TMP_DIR="${GOTMPDIR:-${TMPDIR:-/tmp}/rcod-go-tmp}"

usage() {
  cat <<'EOF'
Usage:
  scripts/install-rcod-systemd.sh [--mode system|user] [--config PATH] [--state-dir PATH] [--bin PATH] [--service-user USER]

Modes:
  system  Install /etc/systemd/system/rcod.service and run RCOD as a non-root user (default)
  user    Install ~/.config/systemd/user/rcod.service for the current user

Examples:
  sudo scripts/install-rcod-systemd.sh --mode system --service-user rcod --config /etc/rcod/config.yaml
  scripts/install-rcod-systemd.sh --mode user --config "$HOME/.config/rcod/config.yaml"
EOF
}

canonicalize_path() {
  local input="$1"
  local dir
  dir="$(cd -- "$(dirname -- "$input")" && pwd -P)"
  printf '%s/%s\n' "$dir" "$(basename -- "$input")"
}

escape_sed_replacement() {
  printf '%s' "$1" | sed 's/[\/&]/\\&/g'
}

ensure_system_user() {
  local user="$1"
  local group="$2"
  local state_dir="$3"

  if id -u "$user" >/dev/null 2>&1; then
    return
  fi

  if ! getent group "$group" >/dev/null 2>&1; then
    groupadd --system "$group"
  fi

  useradd \
    --system \
    --gid "$group" \
    --home-dir "$state_dir" \
    --shell /usr/sbin/nologin \
    --comment "RCOD service user" \
    "$user"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      MODE="${2:-}"
      shift 2
      ;;
    --config)
      CONFIG_PATH="${2:-}"
      shift 2
      ;;
    --state-dir)
      STATE_DIR="${2:-}"
      shift 2
      ;;
    --bin)
      BIN_PATH="${2:-}"
      shift 2
      ;;
    --service-user)
      SERVICE_USER="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

case "$MODE" in
  system|user)
    ;;
  *)
    echo "invalid mode: $MODE" >&2
    usage >&2
    exit 1
    ;;
esac

if [[ "$MODE" == "system" ]]; then
  if [[ "${EUID}" -ne 0 ]]; then
    echo "system mode requires root (run with sudo)" >&2
    exit 1
  fi

  CONFIG_PATH="${CONFIG_PATH:-/etc/rcod/config.yaml}"
  STATE_DIR="${STATE_DIR:-/var/lib/rcod}"
  BIN_PATH="${BIN_PATH:-/usr/local/bin/rcod}"

  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    SERVICE_GROUP="${SERVICE_GROUP:-$(id -gn "$SERVICE_USER")}"
  else
    SERVICE_GROUP="${SERVICE_GROUP:-$SERVICE_USER}"
  fi

  UNIT_TEMPLATE="$ROOT_DIR/packaging/systemd/rcod.service"
  UNIT_PATH="/etc/systemd/system/rcod.service"
  SYSTEMCTL=(systemctl)

  ensure_system_user "$SERVICE_USER" "$SERVICE_GROUP" "$STATE_DIR"

  mkdir -p "$(dirname -- "$CONFIG_PATH")" "$STATE_DIR" "$(dirname -- "$BIN_PATH")"
  chown "$SERVICE_USER:$SERVICE_GROUP" "$STATE_DIR"
  chmod 0700 "$STATE_DIR"

  if [[ -f "$CONFIG_PATH" ]]; then
    chgrp "$SERVICE_GROUP" "$CONFIG_PATH"
    chmod 0640 "$CONFIG_PATH"
  fi
else
  CONFIG_PATH="${CONFIG_PATH:-$HOME/.config/rcod/config.yaml}"
  STATE_DIR="${STATE_DIR:-$HOME/.local/state/rcod}"
  BIN_PATH="${BIN_PATH:-$HOME/.local/bin/rcod}"
  SERVICE_USER="$(id -un)"
  SERVICE_GROUP="$(id -gn)"
  UNIT_TEMPLATE="$ROOT_DIR/packaging/systemd/rcod.user.service"
  UNIT_PATH="$HOME/.config/systemd/user/rcod.service"
  SYSTEMCTL=(systemctl --user)

  mkdir -p "$(dirname -- "$CONFIG_PATH")" "$STATE_DIR" "$(dirname -- "$UNIT_PATH")" "$(dirname -- "$BIN_PATH")"
  chmod 0700 "$STATE_DIR"
fi

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "config not found: $CONFIG_PATH" >&2
  exit 1
fi

if [[ ! -f "$UNIT_TEMPLATE" ]]; then
  echo "systemd template not found: $UNIT_TEMPLATE" >&2
  exit 1
fi

CONFIG_PATH="$(canonicalize_path "$CONFIG_PATH")"
STATE_DIR="$(canonicalize_path "$STATE_DIR")"
BIN_PATH="$(canonicalize_path "$BIN_PATH")"

mkdir -p "$(dirname -- "$UNIT_PATH")"
mkdir -p "$GO_CACHE_DIR" "$GO_TMP_DIR"

echo "building rcod..."
env \
  GOCACHE="$GO_CACHE_DIR" \
  GOMODCACHE="$GO_MOD_CACHE" \
  GOTMPDIR="$GO_TMP_DIR" \
  "$GO_BIN" build -o "$BIN_PATH" ./cmd/codexbot

echo "writing systemd unit to $UNIT_PATH..."
sed \
  -e "s/<SERVICE_USER>/$(escape_sed_replacement "$SERVICE_USER")/g" \
  -e "s/<SERVICE_GROUP>/$(escape_sed_replacement "$SERVICE_GROUP")/g" \
  -e "s/<BIN_PATH>/$(escape_sed_replacement "$BIN_PATH")/g" \
  -e "s/<CONFIG_PATH>/$(escape_sed_replacement "$CONFIG_PATH")/g" \
  -e "s/<STATE_DIR>/$(escape_sed_replacement "$STATE_DIR")/g" \
  "$UNIT_TEMPLATE" >"$UNIT_PATH"

echo "reloading systemd..."
"${SYSTEMCTL[@]}" daemon-reload
"${SYSTEMCTL[@]}" enable --now rcod.service

echo
echo "rcod service installed"
echo "mode: $MODE"
echo "user: $SERVICE_USER"
echo "binary: $BIN_PATH"
echo "config: $CONFIG_PATH"
echo "state dir: $STATE_DIR"
if [[ "$MODE" == "system" ]]; then
  echo "status: systemctl status rcod"
  echo "logs: journalctl -u rcod -f"
else
  echo "status: systemctl --user status rcod"
  echo "logs: journalctl --user -u rcod -f"
  echo "boot persistence: sudo loginctl enable-linger $SERVICE_USER"
fi
