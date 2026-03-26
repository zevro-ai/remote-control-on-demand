#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"$ROOT_DIR/scripts/start-rcodbot-native.sh" "${1:-$ROOT_DIR/config.yaml}"
echo
"$ROOT_DIR/scripts/status-rcodbot-native.sh"
echo "tail logs with: tail -f $ROOT_DIR/.rcodbot/rcodbot.log"
