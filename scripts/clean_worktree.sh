#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "Cleaning build artifacts, temp logs, and debug files under $ROOT_DIR"

rm -rf \
  bin \
  dist \
  internal/alist_source/public/dist \
  internal/i18n

rm -f \
  animate-server \
  animateAutoTool \
  animateTool \
  animate_server \
  animate_tool \
  server \
  server.log \
  server_debug.log \
  server_new.log \
  debug_calendar.html \
  mikan_home.html \
  dist.tar.gz

rm -f logs/*.log 2>/dev/null || true

rm -rf debug_metadata

for db_file in animate.db animate_auto.db data.db; do
  if [[ -f "$db_file" && ! -s "$db_file" ]]; then
    rm -f "$db_file"
  fi
done

echo "Clean complete."
