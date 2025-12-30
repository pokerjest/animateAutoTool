#!/bin/bash
# scripts/restart.sh

cd "$(dirname "$0")/.."
chmod +x scripts/manage.sh
./scripts/manage.sh restart
