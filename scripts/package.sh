#!/bin/bash

# Configuration
APP_NAME="animate-server"
VERSION=${1:-"v0.2.0-beta"}  # Accepts version as argument, default to v0.2.0-beta
DIST_DIR="./dist"
SRC_PATH="cmd/server/main.go"

# Platforms to build for
PLATFORMS=(
    "darwin/amd64"
    "darwin/arm64"
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
)

# Colors
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Clean build directory
rm -rf $DIST_DIR
mkdir -p $DIST_DIR

echo -e "${GREEN}Starting build for version $VERSION...${NC}"

for PLATFORM in "${PLATFORMS[@]}"; do
    IFS="/" read -r OS ARCH <<< "$PLATFORM"
    
    OUTPUT_NAME=$APP_NAME
    if [ "$OS" = "windows" ]; then
        OUTPUT_NAME+=".exe"
    fi

    echo -e "Building for ${GREEN}$OS/$ARCH${NC}..."
    
    # Create temp directory for current platform
    PLATFORM_DIR="$DIST_DIR/${APP_NAME}_${VERSION}_${OS}_${ARCH}"
    mkdir -p $PLATFORM_DIR

    # Build
    env CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -ldflags "-s -w" -o "$PLATFORM_DIR/$OUTPUT_NAME" $SRC_PATH
    
    # Copy assets
    cp -r web "$PLATFORM_DIR/"
    cp config.yaml "$PLATFORM_DIR/"
    cp README.md "$PLATFORM_DIR/"
    # Create empty data dir
    mkdir -p "$PLATFORM_DIR/data"

    # Compress
    if [ "$OS" = "windows" ]; then
        # Use zip for windows
        (cd $DIST_DIR && zip -r "${APP_NAME}_${VERSION}_${OS}_${ARCH}.zip" "${APP_NAME}_${VERSION}_${OS}_${ARCH}")
    else
        # Use tar.gz for others
        (cd $DIST_DIR && tar -czf "${APP_NAME}_${VERSION}_${OS}_${ARCH}.tar.gz" "${APP_NAME}_${VERSION}_${OS}_${ARCH}")
    fi

    # Cleanup temp dir
    rm -rf $PLATFORM_DIR
done

echo -e "${GREEN}Packaging complete! Artifacts are in $DIST_DIR${NC}"
