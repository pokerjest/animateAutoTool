#!/bin/bash
# scripts/stop.sh

cd "$(dirname "$0")/.."
chmod +x scripts/manage.sh
./scripts/manage.sh stop
