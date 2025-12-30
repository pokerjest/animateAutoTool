#!/bin/bash

# Configuration
APP_NAME="animate-server"
VERSION=${1:-"v0.3.0"}
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
    PLATFORM_DIR_NAME="${APP_NAME}_${VERSION}_${OS}_${ARCH}"
    PLATFORM_DIR="$DIST_DIR/$PLATFORM_DIR_NAME"
    mkdir -p $PLATFORM_DIR
    mkdir -p "$PLATFORM_DIR/bin"
    mkdir -p "$PLATFORM_DIR/scripts"
    mkdir -p "$PLATFORM_DIR/logs"
    mkdir -p "$PLATFORM_DIR/data"

    # Build
    # Note: We put the binary in bin/ to match the structure expected by scripts
    env CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -ldflags "-s -w" -o "$PLATFORM_DIR/bin/$OUTPUT_NAME" $SRC_PATH
    
    # Copy Assets
    cp -r web "$PLATFORM_DIR/"
    cp config.yaml.example "$PLATFORM_DIR/"
    if [ -f "config.yaml" ]; then
        # Optional: Don't overwrite if user wants clean install, but for package we usually just give example
        # Let's NOT copy local config.yaml to avoid leaking secrets
        :
    fi
    cp README.md "$PLATFORM_DIR/"
    
    # Copy Scripts
    cp scripts/setup.sh "$PLATFORM_DIR/scripts/"
    cp scripts/manage.sh "$PLATFORM_DIR/scripts/"
    
    # Generate Root Entry Scripts (for package structure)
    
    # start.sh
    cat > "$PLATFORM_DIR/start.sh" <<EOF
#!/bin/bash
cd "\$(dirname "\$0")"
chmod +x scripts/manage.sh
./scripts/manage.sh start
EOF

    # stop.sh
    cat > "$PLATFORM_DIR/stop.sh" <<EOF
#!/bin/bash
cd "\$(dirname "\$0")"
chmod +x scripts/manage.sh
./scripts/manage.sh stop
EOF

    # restart.sh
    cat > "$PLATFORM_DIR/restart.sh" <<EOF
#!/bin/bash
cd "\$(dirname "\$0")"
chmod +x scripts/manage.sh
./scripts/manage.sh restart
EOF

    # run.sh (optional helper)
    cat > "$PLATFORM_DIR/run.sh" <<EOF
#!/bin/bash
cd "\$(dirname "\$0")"
chmod +x scripts/manage.sh
./scripts/manage.sh run
EOF
    
    # Make scripts executable
    chmod +x "$PLATFORM_DIR/scripts/"*.sh
    chmod +x "$PLATFORM_DIR/"*.sh

    if [ "$OS" = "windows" ]; then
        # Copy Windows Batch Scripts
        # We assume the templates are in scripts/*.bat (but I created them there? No I created them in scripts/.. wait I created them in scripts/ via write_to_file but I need to check where I put them)
        # Ah, I see I wrote them to scripts/start.bat etc in the previous step?
        # Let's check the file creation paths.
        # I wrote to scripts/start.bat
        
        cp scripts/start.bat "$PLATFORM_DIR/"
        cp scripts/stop.bat "$PLATFORM_DIR/"
        cp scripts/run.bat "$PLATFORM_DIR/"
        
        # Also maybe a control.bat wrapper? Not strictly needed if we have individual ones.
        # But let's rename start.bat to just "start.bat" (already done)
    fi

    # Compress
    if [ "$OS" = "windows" ]; then
        # Use zip for windows
        (cd $DIST_DIR && zip -r "${PLATFORM_DIR_NAME}.zip" "${PLATFORM_DIR_NAME}")
    else
        # Use tar.gz for others
        (cd $DIST_DIR && tar -czf "${PLATFORM_DIR_NAME}.tar.gz" "${PLATFORM_DIR_NAME}")
    fi

    # Cleanup temp dir
    rm -rf $PLATFORM_DIR
done

echo -e "${GREEN}Packaging complete! Artifacts are in $DIST_DIR${NC}"
