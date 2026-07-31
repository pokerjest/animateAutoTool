#!/bin/bash

set -euo pipefail

RELEASE_ASSET_NAME="AnimateAutoTool"
LAUNCHER_NAME="AnimateAutoTool"
LEGACY_LAUNCHER_NAME="animate-server"
APP_DISPLAY_NAME="Animate Auto Tool"
APP_BUNDLE_NAME="${APP_DISPLAY_NAME}.app"
APP_IDENTIFIER="com.pokerjest.animateautotool"
VERSION_FILE="./VERSION"
DEFAULT_VERSION="v1.0.0-beta.5"
DIST_DIR="${DIST_DIR:-./dist}"
SRC_PATH="./cmd/server"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

DEFAULT_PLATFORMS=(
    "darwin/amd64"
    "darwin/arm64"
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
)

if [ -f "$VERSION_FILE" ]; then
    FILE_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
else
    FILE_VERSION="$DEFAULT_VERSION"
fi
VERSION=${1:-"$FILE_VERSION"}

echo "Building embedded Vue frontend..."
npm --prefix web/frontend ci
npm --prefix web/frontend run build

PACKAGE_INCLUDE_ARCHIVES=${PACKAGE_INCLUDE_ARCHIVES:-1}
PACKAGE_INCLUDE_WINDOWS_STANDALONE=${PACKAGE_INCLUDE_WINDOWS_STANDALONE:-1}
PACKAGE_INCLUDE_DMG=${PACKAGE_INCLUDE_DMG:-auto}
PACKAGE_TARGETS=${PACKAGE_TARGETS:-}
PACKAGE_WINDOWS_GUI=${PACKAGE_WINDOWS_GUI:-1}

read_platforms() {
    if [ -z "$PACKAGE_TARGETS" ]; then
        printf '%s\n' "${DEFAULT_PLATFORMS[@]}"
        return
    fi

    tr ',' '\n' <<< "$PACKAGE_TARGETS" | sed '/^[[:space:]]*$/d'
}

build_binary() {
    local os="$1"
    local arch="$2"
    local output_path="$3"
    local ldflags="-s -w -X github.com/pokerjest/animateAutoTool/internal/version.AppVersion=${VERSION}"
    if [ "$os" = "windows" ] && [ "$PACKAGE_WINDOWS_GUI" = "1" ]; then
        ldflags="$ldflags -H=windowsgui"
    fi

    env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -ldflags "$ldflags" -o "$output_path" "$SRC_PATH"
}

write_unix_entry_script() {
    local target_path="$1"
    local command_name="$2"

    cat > "$target_path" <<EOF
#!/bin/bash
cd "\$(dirname "\$0")"
chmod +x scripts/manage.sh
./scripts/manage.sh $command_name
EOF
}

package_archive() {
    local os="$1"
    local arch="$2"
    local binary_path="$3"
    local output_name="$4"

    local platform_dir_name="${RELEASE_ASSET_NAME}_${VERSION}_${os}_${arch}"
    local platform_dir="$DIST_DIR/$platform_dir_name"

    rm -rf "$platform_dir"
    mkdir -p "$platform_dir/bin" "$platform_dir/scripts" "$platform_dir/logs" "$platform_dir/data"

    cp "$binary_path" "$platform_dir/bin/$output_name"
    local legacy_output_name="$LEGACY_LAUNCHER_NAME"
    if [ "$os" = "windows" ]; then
        legacy_output_name="${legacy_output_name}.exe"
    fi
    if [ "$legacy_output_name" != "$output_name" ]; then
        cp "$binary_path" "$platform_dir/bin/$legacy_output_name"
    fi
    cp config.yaml.example "$platform_dir/"
    cp config.yaml.example "$platform_dir/config.yaml"
    cp README.md "$platform_dir/"
    cp "$DIST_DIR/animate-release-manifest.json" "$platform_dir/"
    cp scripts/setup.sh "$platform_dir/scripts/"
    cp scripts/manage.sh "$platform_dir/scripts/"

    write_unix_entry_script "$platform_dir/start.sh" "start"
    write_unix_entry_script "$platform_dir/stop.sh" "stop"
    write_unix_entry_script "$platform_dir/restart.sh" "restart"
    write_unix_entry_script "$platform_dir/run.sh" "run"

    chmod +x "$platform_dir/scripts/"*.sh
    chmod +x "$platform_dir/"*.sh

    if [ "$os" = "windows" ]; then
        cp scripts/start.bat "$platform_dir/"
        cp scripts/stop.bat "$platform_dir/"
        cp scripts/run.bat "$platform_dir/"
        cp scripts/restart.bat "$platform_dir/"
        cp scripts/open-ui.bat "$platform_dir/"
        cp scripts/open-data.bat "$platform_dir/"
        cp scripts/open-config.bat "$platform_dir/"
        cp scripts/view-logs.bat "$platform_dir/"
        cp scripts/init-config.bat "$platform_dir/"
        cp WINDOWS_QUICKSTART.txt "$platform_dir/"
        (
            cd "$DIST_DIR"
            zip -rq "${platform_dir_name}.zip" "${platform_dir_name}"
        )
    else
        (
            cd "$DIST_DIR"
            tar -czf "${platform_dir_name}.tar.gz" "${platform_dir_name}"
        )
    fi

    rm -rf "$platform_dir"
}

package_windows_standalone() {
    local arch="$1"
    local binary_path="$2"
    local artifact_name="${RELEASE_ASSET_NAME}_${VERSION}_windows_${arch}.exe"

    cp "$binary_path" "$DIST_DIR/$artifact_name"
}

should_build_dmg() {
    case "$PACKAGE_INCLUDE_DMG" in
        1|true|TRUE|yes|YES)
            return 0
            ;;
        0|false|FALSE|no|NO)
            return 1
            ;;
        *)
            command -v hdiutil >/dev/null 2>&1
            ;;
    esac
}

package_macos_dmg() {
    local arch="$1"
    local binary_path="$2"
    local version_number="${VERSION#v}"
    local stage_dir="$DIST_DIR/.dmg_${arch}"
    local app_dir="$stage_dir/$APP_BUNDLE_NAME"
    local contents_dir="$app_dir/Contents"
    local macos_dir="$contents_dir/MacOS"
    local resources_dir="$contents_dir/Resources"
    local dmg_name="${RELEASE_ASSET_NAME}_${VERSION}_darwin_${arch}.dmg"

    rm -rf "$stage_dir"
    mkdir -p "$macos_dir" "$resources_dir"

    cp "$binary_path" "$macos_dir/$LAUNCHER_NAME"
    chmod +x "$macos_dir/$LAUNCHER_NAME"
    cp config.yaml.example "$resources_dir/"
    cp README.md "$resources_dir/"
    cp "$DIST_DIR/animate-release-manifest.json" "$resources_dir/"
    if [ -f "internal/tray/icon.png" ]; then
        cp internal/tray/icon.png "$resources_dir/app_icon.png"
    fi
    if [ -f "internal/tray/icon.icns" ]; then
        cp internal/tray/icon.icns "$resources_dir/app_icon.icns"
    fi

    cat > "$contents_dir/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDisplayName</key>
    <string>${APP_DISPLAY_NAME}</string>
    <key>CFBundleExecutable</key>
    <string>${LAUNCHER_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>${APP_IDENTIFIER}</string>
    <key>CFBundleName</key>
    <string>${APP_DISPLAY_NAME}</string>
    <key>CFBundleIconFile</key>
    <string>app_icon</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${version_number}</string>
    <key>CFBundleVersion</key>
    <string>${version_number}</string>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
</dict>
</plist>
EOF

    cp README.md "$stage_dir/"
    cp config.yaml.example "$stage_dir/"
    ln -s /Applications "$stage_dir/Applications"

    rm -f "$DIST_DIR/$dmg_name"
    hdiutil create \
        -volname "${APP_DISPLAY_NAME} ${VERSION}" \
        -srcfolder "$stage_dir" \
        -ov \
        -format UDZO \
        "$DIST_DIR/$dmg_name" >/dev/null

    rm -rf "$stage_dir"
}

generate_checksums() {
    local output_file="$DIST_DIR/SHA256SUMS.txt"

    if command -v shasum >/dev/null 2>&1; then
        (
            cd "$DIST_DIR"
            shasum -a 256 * > "$(basename "$output_file")"
        )
        return
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        (
            cd "$DIST_DIR"
            sha256sum * > "$(basename "$output_file")"
        )
        return
    fi

    echo -e "${YELLOW}No checksum tool found (shasum/sha256sum). Skipping SHA256SUMS generation.${NC}"
}

write_release_manifest() {
    local channel="stable"
    if [[ "$VERSION" == *-* ]]; then
        channel="beta"
    fi
    cat > "$DIST_DIR/animate-release-manifest.json" <<EOF
{
  "format_version": 1,
  "version": "$VERSION",
  "channel": "$channel",
  "schema_version": "014",
  "min_upgrade_from": "0.9.0",
  "min_readable_schema": "001",
  "max_readable_schema": "014",
  "switchable_from_prerelease": true,
  "rollback_supported": false
}
EOF
}

case "$DIST_DIR" in
    ""|"/"|"."|"./")
        echo "Refusing to use unsafe package output directory: $DIST_DIR" >&2
        exit 1
        ;;
esac

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"
write_release_manifest

echo -e "${GREEN}Starting build for version $VERSION...${NC}"

while IFS= read -r platform; do
    [ -n "$platform" ] || continue

    IFS="/" read -r os arch <<< "$platform"
    output_name="$LAUNCHER_NAME"
    if [ "$os" = "windows" ]; then
        output_name="${output_name}.exe"
    fi

    build_output="$DIST_DIR/.build_${os}_${arch}_${output_name}"

    echo -e "Building for ${GREEN}$os/$arch${NC}..."
    build_binary "$os" "$arch" "$build_output"

    if [ "$PACKAGE_INCLUDE_ARCHIVES" = "1" ]; then
        package_archive "$os" "$arch" "$build_output" "$output_name"
    fi

    if [ "$PACKAGE_INCLUDE_WINDOWS_STANDALONE" = "1" ] && [ "$os" = "windows" ]; then
        package_windows_standalone "$arch" "$build_output"
    fi

    if [ "$os" = "darwin" ] && should_build_dmg; then
        package_macos_dmg "$arch" "$build_output"
    fi

    rm -f "$build_output"
done < <(read_platforms)

generate_checksums

if ! should_build_dmg; then
    echo -e "${YELLOW}Skipping DMG packaging because hdiutil is unavailable or disabled.${NC}"
fi

echo -e "${GREEN}Packaging complete! Artifacts are in $DIST_DIR${NC}"
