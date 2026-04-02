#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
LINTER="$ROOT_DIR/.tools/bin/golangci-lint"

if [ ! -x "$LINTER" ]; then
    echo "golangci-lint not found at $LINTER" >&2
    echo "Install it first with:" >&2
    echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4" >&2
    exit 1
fi

export GOTOOLCHAIN=local
export GOTELEMETRY=off
export GOMODCACHE="$ROOT_DIR/.gomodcache"
export GOCACHE="$ROOT_DIR/.gocache"
export GOLANGCI_LINT_CACHE="$ROOT_DIR/.lintcache"

cd "$ROOT_DIR"
if [ "$#" -eq 0 ]; then
    exec "$LINTER" run --timeout=5m
fi

exec "$LINTER" "$@"
