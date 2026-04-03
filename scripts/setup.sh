#!/bin/bash
# scripts/setup.sh

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Initializing Animate Auto Tool Environment...${NC}"

# 1. Check Go Version
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: 'go' is not installed.${NC}"
    echo "Please install Go 1.25+ from https://go.dev/dl/"
    exit 1
fi

REQUIRED_GO="1.25"
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo -e "Found Go version: ${GREEN}$GO_VERSION${NC}"

if [[ "$(printf '%s\n%s\n' "$REQUIRED_GO" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_GO" ]]; then
    echo -e "${RED}Error: Go ${REQUIRED_GO}+ is required. Found ${GO_VERSION}.${NC}"
    exit 1
fi

# 2. Create Directories
echo "Creating directories..."
mkdir -p logs
mkdir -p data
mkdir -p bin

# 3. Setup Config
if [ ! -f config.yaml ]; then
    if [ -f config.yaml.example ]; then
        echo "Creating config.yaml from example..."
        cp config.yaml.example config.yaml
        echo -e "${GREEN}Created config.yaml. Please edit it with your settings.${NC}"
    else
        echo -e "${RED}Error: config.yaml.example not found!${NC}"
    fi
else
    echo "config.yaml already exists. Skipping."
fi

# 4. Make scripts executable
chmod +x scripts/*.sh 2>/dev/null

echo -e "${GREEN}Setup complete!${NC}"
echo -e "Run ${GREEN}./scripts/start.sh${NC} to start the server."
