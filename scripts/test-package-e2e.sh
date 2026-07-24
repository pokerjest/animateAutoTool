#!/bin/bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
FRONTEND_DIR="$ROOT_DIR/web/frontend"
VERSION_FILE="$ROOT_DIR/VERSION"
VERSION=${PACKAGE_E2E_VERSION:-$(tr -d '[:space:]' < "$VERSION_FILE")}
PORT=${PACKAGE_E2E_PORT:-18306}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/animate-package-e2e.XXXXXX")
ARTIFACT_DIR="$WORK_DIR/artifacts"
UNPACK_DIR="$WORK_DIR/unpacked"
SERVER_LOG="$WORK_DIR/server.log"
RESULTS_DIR="$FRONTEND_DIR/test-results"
SERVER_PID=""

cleanup() {
    local status=$?
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    if [ "$status" -ne 0 ] && [ -f "$SERVER_LOG" ]; then
        mkdir -p "$RESULTS_DIR"
        cp "$SERVER_LOG" "$RESULTS_DIR/package-e2e-server.log"
        echo "Packaged server log:" >&2
        tail -n 200 "$SERVER_LOG" >&2 || true
    fi
    if [ "${PACKAGE_E2E_KEEP_WORKDIR:-0}" = "1" ]; then
        echo "Package E2E work directory retained at $WORK_DIR"
    else
        rm -rf "$WORK_DIR"
    fi
    return "$status"
}
trap cleanup EXIT INT TERM

host_os() {
    case "$(uname -s)" in
        Linux) echo "linux" ;;
        Darwin) echo "darwin" ;;
        *) echo "unsupported" ;;
    esac
}

host_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *) echo "unsupported" ;;
    esac
}

OS=$(host_os)
ARCH=$(host_arch)
if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
    echo "Package E2E currently supports Linux and macOS on amd64/arm64 hosts." >&2
    exit 1
fi

mkdir -p "$ARTIFACT_DIR" "$UNPACK_DIR"

ARTIFACT=${PACKAGE_E2E_ARTIFACT:-}
if [ -z "$ARTIFACT" ]; then
    echo "Building the $OS/$ARCH release archive for E2E validation..."
    (
        cd "$ROOT_DIR"
        DIST_DIR="$ARTIFACT_DIR" \
        PACKAGE_TARGETS="$OS/$ARCH" \
        PACKAGE_INCLUDE_ARCHIVES=1 \
        PACKAGE_INCLUDE_WINDOWS_STANDALONE=0 \
        PACKAGE_INCLUDE_DMG=0 \
        bash ./scripts/package.sh "$VERSION"
    )
    ARTIFACT="$ARTIFACT_DIR/animate-server_${VERSION}_${OS}_${ARCH}.tar.gz"
elif [[ "$ARTIFACT" != /* ]]; then
    ARTIFACT="$ROOT_DIR/$ARTIFACT"
fi

if [ ! -f "$ARTIFACT" ]; then
    echo "Packaged archive not found: $ARTIFACT" >&2
    exit 1
fi

case "$ARTIFACT" in
    *.tar.gz) tar -xzf "$ARTIFACT" -C "$UNPACK_DIR" ;;
    *.zip) unzip -q "$ARTIFACT" -d "$UNPACK_DIR" ;;
    *)
        echo "Unsupported packaged archive: $ARTIFACT" >&2
        exit 1
        ;;
esac

PACKAGE_DIR=$(find "$UNPACK_DIR" -mindepth 1 -maxdepth 1 -type d -name 'animate-server_*' -print -quit)
if [ -z "$PACKAGE_DIR" ]; then
    echo "The archive does not contain the expected release directory." >&2
    exit 1
fi

BINARY="$PACKAGE_DIR/bin/animate-server"
if [ ! -f "$BINARY" ]; then
    BINARY="$PACKAGE_DIR/bin/animate-server.exe"
fi
if [ ! -f "$BINARY" ]; then
    echo "The archive does not contain the server binary." >&2
    exit 1
fi
chmod +x "$BINARY"

# Prevent the isolated smoke test from downloading or starting a real managed
# qBittorrent process. The tiny executable exits immediately after launch.
QB_STUB="$PACKAGE_DIR/bin/qbittorrent-nox"
printf '#!/bin/sh\nexit 0\n' > "$QB_STUB"
chmod +x "$QB_STUB"

if [ ! -x "$FRONTEND_DIR/node_modules/.bin/playwright" ]; then
    npm --prefix "$FRONTEND_DIR" ci
fi
if [ "${PACKAGE_E2E_INSTALL_BROWSER:-1}" = "1" ]; then
    npm --prefix "$FRONTEND_DIR" exec -- playwright install chromium
fi

echo "Starting packaged server on http://127.0.0.1:$PORT ..."
(
    cd "$PACKAGE_DIR"
    exec env \
        ANIME_SERVER_PORT="$PORT" \
        ANIME_SERVER_HEADLESS=true \
        ANIME_MANAGED_SERVICES_DOWNLOAD_MISSING=false \
        "$BINARY"
) >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

ready=0
for _ in $(seq 1 120); do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        break
    fi
    if curl -fsS "http://127.0.0.1:$PORT/api/v1/session" >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 0.25
done
if [ "$ready" -ne 1 ]; then
    echo "Packaged server did not become ready." >&2
    exit 1
fi

export PACKAGE_E2E_BASE_URL="http://127.0.0.1:$PORT"
export PACKAGE_E2E_VERSION="$VERSION"
(
    cd "$FRONTEND_DIR"
    npm exec -- playwright test --config e2e/playwright.config.ts
)

echo "Packaged release passed headless E2E validation."
