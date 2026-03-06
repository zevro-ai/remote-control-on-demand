#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"$ROOT_DIR/scripts/start-codexbot-native.sh" "${1:-$ROOT_DIR/config.yaml}"
echo
"$ROOT_DIR/scripts/status-codexbot-native.sh"
echo "tail logs with: tail -f $ROOT_DIR/.codexbot/codexbot.log"
