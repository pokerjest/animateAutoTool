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
    echo "Please install Go 1.24+ from https://go.dev/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo -e "Found Go version: ${GREEN}$GO_VERSION${NC}"

# Simple version check using sort -V (version sort)
if [[ $(echo -e "1.24\n$GO_VERSION" | sort -V | head -n1) != "1.24" ]]; then
    # This check is basic; if version is like 1.25 it will pass, if 1.23, it fails.
    # Note: sort -V might not be available on all minimal systems, but common on dev machines.
    # Fallback to simple major.minor parsing if needed, but let's stick to simple logic first.
    : # Version OK or newer
else
    # Double check if it really IS strictly less than 1.24
    if [[ "$GO_VERSION" < "1.24" ]]; then
         echo -e "${RED}Warning: Go 1.24+ is recommended. Found $GO_VERSION.${NC}"
    fi
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
echo -e "Run ${GREEN}./start.sh${NC} to start the server."
