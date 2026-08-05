#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${ANIMATE_HISTORICAL_FIXTURE_DIR:-$ROOT_DIR/testdata/historical}"
WRITE_FIXTURES="${ANIMATE_HISTORICAL_WRITE_FIXTURES:-0}"
TAGS=(v0.9.9 v1.0.0-beta.1 v1.0.0-beta.7 v1.0.0-beta.14 v1.0.0-beta.15)

command -v git >/dev/null
command -v go >/dev/null

mkdir -p "$OUTPUT_DIR"
helper="$ROOT_DIR/internal/db/historical_fixture_builder_test.go"

for tag in "${TAGS[@]}"; do
    if ! git rev-parse --verify --quiet "$tag^{commit}" >/dev/null; then
        echo "missing required historical tag: $tag" >&2
        exit 1
    fi
    worktree="$(mktemp -d "${TMPDIR:-/tmp}/animate-historical-XXXXXX")"
    fixture="$(mktemp "${TMPDIR:-/tmp}/animate-historical-source-XXXXXX.db")"
    upgraded_fixture=""
    cleanup() {
        git worktree remove --force "$worktree" >/dev/null 2>&1 || true
        rm -rf "$worktree"
        rm -f "$fixture" "$upgraded_fixture"
    }
    trap cleanup EXIT
    git archive "$tag" | tar -x -C "$worktree"
    cp "$helper" "$worktree/internal/db/historical_fixture_builder_test.go"
    (
        cd "$worktree"
        ANIMATE_HISTORICAL_OUTPUT="$fixture" \
            GOCACHE="${GOCACHE:-/tmp/animate-historical-go-cache}" \
            go test ./internal/db -tags historical_fixture_builder -run TestBuildHistoricalFixture -count=1
    )
    rm -rf "$worktree"
    upgraded_fixture="$(mktemp "${TMPDIR:-/tmp}/animate-historical-upgraded-XXXXXX.db")"
    (
        cd "$ROOT_DIR"
            ANIMATE_HISTORICAL_INPUT="$fixture" \
            ANIMATE_HISTORICAL_OUTPUT="$upgraded_fixture" \
            GOCACHE="${GOCACHE:-/tmp/animate-historical-current-cache}" \
            go test ./internal/db -tags historical_fixture_builder -run TestBuildHistoricalFixture -count=1
    )
    if [[ "$WRITE_FIXTURES" == "1" ]]; then
        mkdir -p "$OUTPUT_DIR"
        cp "$upgraded_fixture" "$OUTPUT_DIR/${tag#v}.db"
    fi
    rm -f "$fixture" "$upgraded_fixture"
    trap - EXIT
    echo "historical upgrade OK: $tag"
done
