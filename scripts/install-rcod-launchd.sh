#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "$SCRIPT_DIR/.." && pwd)"
PACKAGED_TEMPLATE_DIR="/usr/share/rcod/launchd"
MODE="daemon"
SERVICE_USER=""
CONFIG_PATH=""
STATE_DIR=""
LOG_DIR=""
BIN_PATH=""
SKIP_BUILD="false"
DEFAULT_GO_BIN="$(command -v go 2>/dev/null || true)"
GO_BIN="${GO_BIN:-${DEFAULT_GO_BIN:-/usr/local/go/bin/go}}"
GO_CACHE_DIR="${GOCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/rcod/go-build}"
GO_MOD_CACHE="${GOMODCACHE:-$HOME/go/pkg/mod}"
GO_TMP_DIR="${GOTMPDIR:-${TMPDIR:-/tmp}/rcod-go-tmp}"

usage() {
  cat <<'EOF'
Usage:
  scripts/install-rcod-launchd.sh [--mode daemon|agent] [--service-user USER] [--config PATH] [--state-dir PATH] [--log-dir PATH] [--bin PATH] [--skip-build]

Modes:
  daemon  Install /Library/LaunchDaemons/ai.zevro.rcod.plist and run RCOD at boot as a non-root macOS user (default)
  agent   Install ~/Library/LaunchAgents/ai.zevro.rcod.agent.plist for the current user

Examples:
  sudo scripts/install-rcod-launchd.sh --mode daemon --service-user "$USER" --config /usr/local/etc/rcod/config.yaml
  scripts/install-rcod-launchd.sh --mode agent --config "$HOME/.config/rcod/config.yaml"
  sudo scripts/install-rcod-launchd.sh --mode daemon --service-user "$USER" --skip-build --bin /opt/homebrew/bin/rcod
EOF
}

canonicalize_path() {
  local input="$1"
  local parent_dir
  local dir
  parent_dir="$(dirname -- "$input")"
  if [[ ! -d "$parent_dir" ]]; then
    echo "path parent directory does not exist: $parent_dir" >&2
    return 1
  fi
  dir="$(cd -- "$parent_dir" && pwd -P)"
  printf '%s/%s\n' "$dir" "$(basename -- "$input")"
}

escape_sed_replacement() {
  printf '%s' "$1" | sed 's/[\\/&]/\\&/g'
}

resolve_template_path() {
  local filename="$1"

  if [[ -f "$ROOT_DIR/packaging/launchd/$filename" ]]; then
    printf '%s\n' "$ROOT_DIR/packaging/launchd/$filename"
    return
  fi

  if [[ -f "$PACKAGED_TEMPLATE_DIR/$filename" ]]; then
    printf '%s\n' "$PACKAGED_TEMPLATE_DIR/$filename"
    return
  fi

  echo "launchd template not found: $filename" >&2
  exit 1
}

resolve_user_home() {
  local user="$1"
  local home_dir=""

  if ! command -v dscl >/dev/null 2>&1; then
    echo "dscl is required to resolve macOS home directories" >&2
    exit 1
  fi

  home_dir="$(dscl . -read "/Users/$user" NFSHomeDirectory 2>/dev/null | sed -n 's/^NFSHomeDirectory:[[:space:]]*//p')"
  if [[ -z "$home_dir" || ! -d "$home_dir" ]]; then
    echo "failed to resolve home directory for user: $user" >&2
    exit 1
  fi

  printf '%s\n' "$home_dir"
}

default_prefix() {
  if [[ -x /opt/homebrew/bin/rcod || -d /opt/homebrew/bin || "$(uname -m)" == "arm64" ]]; then
    printf '%s\n' "/opt/homebrew"
    return
  fi

  printf '%s\n' "/usr/local"
}

assert_macos() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "launchd installer only supports macOS" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      MODE="${2:-}"
      shift 2
      ;;
    --service-user)
      SERVICE_USER="${2:-}"
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
    --log-dir)
      LOG_DIR="${2:-}"
      shift 2
      ;;
    --bin)
      BIN_PATH="${2:-}"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD="true"
      shift
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

assert_macos

case "$MODE" in
  daemon|agent)
    ;;
  *)
    echo "invalid mode: $MODE" >&2
    usage >&2
    exit 1
    ;;
esac

PREFIX="$(default_prefix)"

if [[ "$MODE" == "daemon" ]]; then
  if [[ "${EUID}" -ne 0 ]]; then
    echo "daemon mode requires root (run with sudo)" >&2
    exit 1
  fi

  if [[ -z "$SERVICE_USER" ]]; then
    if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]]; then
      SERVICE_USER="${SUDO_USER}"
    else
      echo "daemon mode requires --service-user with a non-root macOS account" >&2
      exit 1
    fi
  fi

  if [[ "$SERVICE_USER" == "root" ]]; then
    echo "RCOD must not run as root; choose a non-root --service-user" >&2
    exit 1
  fi

  if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    echo "service user does not exist: $SERVICE_USER" >&2
    exit 1
  fi

  SERVICE_GROUP="$(id -gn "$SERVICE_USER")"
  USER_HOME="$(resolve_user_home "$SERVICE_USER")"
  CONFIG_PATH="${CONFIG_PATH:-$PREFIX/etc/rcod/config.yaml}"
  STATE_DIR="${STATE_DIR:-$USER_HOME/Library/Application Support/rcod}"
  LOG_DIR="${LOG_DIR:-$USER_HOME/Library/Logs/rcod}"
  BIN_PATH="${BIN_PATH:-$PREFIX/bin/rcod}"
  LABEL="ai.zevro.rcod"
  TEMPLATE_PATH="$(resolve_template_path "ai.zevro.rcod.plist")"
  PLIST_PATH="/Library/LaunchDaemons/$LABEL.plist"
  DOMAIN="system"
  TARGET="$DOMAIN/$LABEL"

  mkdir -p "$(dirname -- "$CONFIG_PATH")" "$STATE_DIR" "$LOG_DIR" "$(dirname -- "$BIN_PATH")" "$(dirname -- "$PLIST_PATH")"
  chown "$SERVICE_USER:$SERVICE_GROUP" "$STATE_DIR" "$LOG_DIR"
  chmod 0700 "$STATE_DIR" "$LOG_DIR"

  if [[ -f "$CONFIG_PATH" ]]; then
    chown "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_PATH"
    chmod 0600 "$CONFIG_PATH"
  fi
else
  if [[ "${EUID}" -eq 0 ]]; then
    echo "agent mode should run as the target user, not root" >&2
    exit 1
  fi

  SERVICE_USER="$(id -un)"
  USER_HOME="$HOME"
  CONFIG_PATH="${CONFIG_PATH:-$HOME/.config/rcod/config.yaml}"
  STATE_DIR="${STATE_DIR:-$HOME/Library/Application Support/rcod}"
  LOG_DIR="${LOG_DIR:-$HOME/Library/Logs/rcod}"
  if [[ -z "$BIN_PATH" ]]; then
    if [[ "$SKIP_BUILD" == "true" ]]; then
      BIN_PATH="$PREFIX/bin/rcod"
    else
      BIN_PATH="$HOME/.local/bin/rcod"
    fi
  fi
  LABEL="ai.zevro.rcod.agent"
  TEMPLATE_PATH="$(resolve_template_path "ai.zevro.rcod.agent.plist")"
  PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"
  DOMAIN="gui/$(id -u)"
  TARGET="$DOMAIN/$LABEL"

  mkdir -p "$(dirname -- "$CONFIG_PATH")" "$STATE_DIR" "$LOG_DIR" "$(dirname -- "$BIN_PATH")" "$(dirname -- "$PLIST_PATH")"
  chmod 0700 "$STATE_DIR" "$LOG_DIR"
fi

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "config not found: $CONFIG_PATH" >&2
  exit 1
fi

CONFIG_PATH="$(canonicalize_path "$CONFIG_PATH")"
STATE_DIR="$(canonicalize_path "$STATE_DIR")"
LOG_DIR="$(canonicalize_path "$LOG_DIR")"
BIN_PATH="$(canonicalize_path "$BIN_PATH")"

if [[ "$SKIP_BUILD" == "true" ]]; then
  if [[ ! -x "$BIN_PATH" ]]; then
    echo "installed rcod binary not found or not executable: $BIN_PATH" >&2
    exit 1
  fi
  echo "using existing rcod binary at $BIN_PATH..."
else
  if [[ ! -f "$ROOT_DIR/go.mod" ]] || [[ ! -d "$ROOT_DIR/cmd/rcodbot" ]]; then
    echo "source build mode requires running from a repository checkout; use --skip-build for prebuilt installs" >&2
    exit 1
  fi

  if [[ ! -x "$GO_BIN" ]]; then
    echo "go toolchain not found: $GO_BIN" >&2
    exit 1
  fi

  mkdir -p "$GO_CACHE_DIR" "$GO_TMP_DIR"

  echo "building rcod..."
  env \
    GOCACHE="$GO_CACHE_DIR" \
    GOMODCACHE="$GO_MOD_CACHE" \
    GOTMPDIR="$GO_TMP_DIR" \
    "$GO_BIN" -C "$ROOT_DIR" build -o "$BIN_PATH" ./cmd/rcodbot
fi

echo "writing launchd plist to $PLIST_PATH..."
sed \
  -e "s/@BIN_PATH@/$(escape_sed_replacement "$BIN_PATH")/g" \
  -e "s/@CONFIG_PATH@/$(escape_sed_replacement "$CONFIG_PATH")/g" \
  -e "s/@STATE_DIR@/$(escape_sed_replacement "$STATE_DIR")/g" \
  -e "s/@LOG_DIR@/$(escape_sed_replacement "$LOG_DIR")/g" \
  -e "s/@SERVICE_USER@/$(escape_sed_replacement "$SERVICE_USER")/g" \
  -e "s/@USER_HOME@/$(escape_sed_replacement "$USER_HOME")/g" \
  "$TEMPLATE_PATH" >"$PLIST_PATH"

if [[ "$MODE" == "daemon" ]]; then
  chown root:wheel "$PLIST_PATH"
fi
chmod 0644 "$PLIST_PATH"

echo "reloading launchd service..."
launchctl bootout "$TARGET" >/dev/null 2>&1 || true

if ! launchctl bootstrap "$DOMAIN" "$PLIST_PATH"; then
  echo "bootstrap failed, retrying once..."
  sleep 1
  launchctl bootstrap "$DOMAIN" "$PLIST_PATH"
fi

launchctl enable "$TARGET" >/dev/null 2>&1 || true
launchctl kickstart -k "$TARGET"

echo
echo "rcod launchd service installed"
echo "mode: $MODE"
echo "label: $LABEL"
echo "user: $SERVICE_USER"
echo "binary: $BIN_PATH"
echo "config: $CONFIG_PATH"
echo "state dir: $STATE_DIR"
echo "log dir: $LOG_DIR"
echo "plist: $PLIST_PATH"
if [[ "$MODE" == "daemon" ]]; then
  echo "status: sudo launchctl print $TARGET"
  echo "restart: sudo launchctl kickstart -k $TARGET"
  echo "stop: sudo launchctl bootout $TARGET"
  echo "start: sudo launchctl bootstrap $DOMAIN $PLIST_PATH"
else
  echo "status: launchctl print $TARGET"
  echo "restart: launchctl kickstart -k $TARGET"
  echo "stop: launchctl bootout $TARGET"
  echo "start: launchctl bootstrap $DOMAIN $PLIST_PATH"
fi
echo "logs: tail -f $LOG_DIR/rcod.stdout.log $LOG_DIR/rcod.stderr.log"
