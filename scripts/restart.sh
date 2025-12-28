#!/bin/bash
# One-click compile and restart script
cd "$(dirname "$0")/.."
chmod +x scripts/control.sh
echo "=========================================="
echo "      Animate Auto Tool - Restart"
echo "=========================================="
echo ""
echo "Stopping and Rebuilding..."
./scripts/control.sh restart
