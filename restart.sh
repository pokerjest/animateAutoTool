#!/bin/bash
# One-click compile and restart script
chmod +x scripts/control.sh
echo "=========================================="
echo "      Animate Auto Tool - Restart"
echo "=========================================="
echo ""
echo "Stopping and Rebuilding..."
./scripts/control.sh restart
