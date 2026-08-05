#!/bin/bash

set -euo pipefail

APP_NAME="${APP_NAME:-AnimateAutoTool}"
VERSION_FILE="${VERSION_FILE:-./VERSION}"
DIST_DIR="${DIST_DIR:-./dist}"
WINDOWS_ARCHES="${WINDOWS_ARCHES:-amd64}"
DARWIN_ARCHES="${DARWIN_ARCHES:-amd64,arm64}"
LINUX_ARCHES="${LINUX_ARCHES:-amd64,arm64}"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

version_arg="${1:-}"
if [ -n "$version_arg" ]; then
    VERSION="$version_arg"
elif [ -f "$VERSION_FILE" ]; then
    VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
else
    echo -e "${RED}ERROR:${NC} VERSION is empty and $VERSION_FILE not found."
    exit 1
fi

if [ -z "$VERSION" ]; then
    echo -e "${RED}ERROR:${NC} VERSION is empty."
    exit 1
fi

if [ ! -d "$DIST_DIR" ]; then
    echo -e "${RED}ERROR:${NC} dist directory not found: $DIST_DIR"
    exit 1
fi

checksum_file="$DIST_DIR/SHA256SUMS.txt"

trim_csv_items() {
    tr ',' '\n' <<< "$1" | sed '/^[[:space:]]*$/d' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

escape_regex() {
    sed -e 's/[]\/$*.^|[]/\\&/g'
}

asset_exists() {
    local file_name="$1"
    [ -f "$DIST_DIR/$file_name" ]
}

checksum_has_entry() {
    local file_name="$1"
    local escaped
    escaped="$(printf '%s' "$file_name" | escape_regex)"
    grep -Eq "^[0-9A-Fa-f]{64}[[:space:]]+[*]?${escaped}$" "$checksum_file"
}

expected_assets=()
expected_assets+=("animate-release-manifest.json")
while IFS= read -r arch; do
    expected_assets+=("${APP_NAME}_${VERSION}_windows_${arch}.exe")
done < <(trim_csv_items "$WINDOWS_ARCHES")

while IFS= read -r arch; do
    expected_assets+=("${APP_NAME}_${VERSION}_linux_${arch}.tar.gz")
done < <(trim_csv_items "$LINUX_ARCHES")

while IFS= read -r arch; do
    expected_assets+=("${APP_NAME}_${VERSION}_darwin_${arch}.dmg")
    expected_assets+=("${APP_NAME}_${VERSION}_darwin_${arch}.tar.gz")
done < <(trim_csv_items "$DARWIN_ARCHES")

echo -e "${GREEN}Release update-assets checklist${NC}"
echo "Version: $VERSION"
echo "DistDir:  $DIST_DIR"
echo

fail=0

echo "1) Required updater assets"
for asset in "${expected_assets[@]}"; do
    if asset_exists "$asset"; then
        echo -e "  - ${GREEN}OK${NC}   $asset"
    else
        echo -e "  - ${RED}MISS${NC} $asset"
        fail=1
    fi
done

echo
echo "2) Compatibility manifest"
manifest_path="$DIST_DIR/animate-release-manifest.json"
if [ -f "$manifest_path" ]; then
    if python3 - "$manifest_path" "$VERSION" <<'PY'
import json
import re
import sys

path, expected = sys.argv[1:3]
with open(path, "r", encoding="utf-8") as handle:
    manifest = json.load(handle)

required = (
    "format_version",
    "version",
    "channel",
    "database_format",
    "schema_format",
    "schema_version",
    "min_upgrade_from",
    "min_readable_schema",
    "max_readable_schema",
    "switchable_from_prerelease",
    "rollback_supported",
    "rollback_scope",
)
missing = [key for key in required if key not in manifest]
if missing:
    raise SystemExit(f"missing fields: {', '.join(missing)}")

normalize = lambda value: str(value).removeprefix("v")
if normalize(manifest["version"]) != normalize(expected):
    raise SystemExit("manifest version does not match requested package version")
if manifest["format_version"] != 1:
    raise SystemExit("unsupported manifest format_version")
if manifest["database_format"] != 1 or manifest["schema_format"] != 1:
    raise SystemExit("unsupported database/schema format")
if manifest["rollback_supported"] and manifest["rollback_scope"] != "bundle_snapshot":
    raise SystemExit("rollback-supported manifest must declare bundle_snapshot scope")
if manifest["channel"] not in ("stable", "beta"):
    raise SystemExit("manifest channel must be stable or beta")
if manifest["channel"] == "beta" and "-" not in normalize(expected):
    raise SystemExit("beta manifest requires a prerelease version")
if manifest["channel"] == "stable" and "-" in normalize(expected):
    raise SystemExit("stable manifest cannot describe a prerelease version")
if normalize(expected) == "1.0.0" and normalize(manifest["min_upgrade_from"]) != "0.9.9":
    raise SystemExit("v1.0.0 manifest must require min_upgrade_from 0.9.9")
for key in ("schema_version", "min_readable_schema", "max_readable_schema"):
    if not re.fullmatch(r"\d+(?:_[A-Za-z0-9.-]+)?", str(manifest[key])):
        raise SystemExit(f"invalid schema field: {key}")
PY
    then
        echo -e "  - ${GREEN}OK${NC}   manifest fields and version"
    else
        echo -e "  - ${RED}FAIL${NC} manifest validation"
        fail=1
    fi
else
    echo -e "  - ${RED}MISS${NC} animate-release-manifest.json"
    fail=1
fi

echo
echo "3) SHA256SUMS.txt"
if [ -f "$checksum_file" ]; then
    echo -e "  - ${GREEN}OK${NC}   SHA256SUMS.txt exists"
else
    echo -e "  - ${RED}MISS${NC} SHA256SUMS.txt"
    fail=1
fi

if [ -f "$checksum_file" ]; then
    echo
    echo "4) Checksum entries for updater assets"
    for asset in "${expected_assets[@]}"; do
        if ! asset_exists "$asset"; then
            echo -e "  - ${YELLOW}SKIP${NC} $asset (asset missing)"
            continue
        fi
        if checksum_has_entry "$asset"; then
            echo -e "  - ${GREEN}OK${NC}   $asset"
        else
            echo -e "  - ${RED}MISS${NC} $asset"
            fail=1
        fi
    done
fi

echo
if [ "$fail" -eq 0 ]; then
    echo -e "${GREEN}PASS:${NC} updater-related release assets look good."
    exit 0
fi

echo -e "${RED}FAIL:${NC} release assets are incomplete for auto-update."
exit 1
