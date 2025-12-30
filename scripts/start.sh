#!/bin/bash
# scripts/start.sh

cd "$(dirname "$0")/.."
chmod +x scripts/manage.sh
./scripts/manage.sh start
