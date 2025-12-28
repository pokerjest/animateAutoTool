#!/bin/bash

# 配置
APP_NAME="animate-server"
BIN_DIR="./bin"
BIN_PATH="$BIN_DIR/$APP_NAME"
PID_FILE="$BIN_DIR/server.pid"
LOG_FILE="server.log"
SRC_PATH="cmd/server/main.go"

# 颜色
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

mkdir -p $BIN_DIR

check_deps() {
    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: 'go' is not installed or not in your PATH.${NC}"
        echo "Please install Go 1.24+ from https://go.dev/dl/"
        exit 1
    fi
    
    # Check Go version
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    # Simple version check (major.minor)
    # Assumes version format like 1.24.0 or 1.24rc1
    MAJOR=$(echo $GO_VERSION | cut -d. -f1)
    MINOR=$(echo $GO_VERSION | cut -d. -f2 | sed 's/[^0-9].*//')
    
    if [ "$MAJOR" -lt 1 ] || ([ "$MAJOR" -eq 1 ] && [ "$MINOR" -lt 24 ]); then
        echo -e "${RED}Error: Go version 1.24 or higher is required.${NC}"
        echo -e "Current version: ${GREEN}$GO_VERSION${NC}"
        echo "Please upgrade Go from https://go.dev/dl/"
        exit 1
    fi
}

build() {
    check_deps
    echo -e "${GREEN}Building $APP_NAME...${NC}"
    # Default to CGO_ENABLED=0 for portability (using pure Go SQLite driver)
    CGO_ENABLED=0 go build -ldflags="-s -w" -o $BIN_PATH $SRC_PATH
    if [ $? -ne 0 ]; then
        echo -e "${RED}Build failed!${NC}"
        echo -e "If you are in China, you may need to set GOPROXY:"
        echo -e "${GREEN}export GOPROXY=https://goproxy.cn,direct${NC}"
        exit 1
    fi
    echo -e "${GREEN}Build successful.${NC}"
}

start() {
    if [ -f $PID_FILE ]; then
        PID=$(cat $PID_FILE)
        if ps -p $PID > /dev/null; then
            echo -e "${RED}$APP_NAME is already running (PID: $PID).${NC}"
            return
        else
            echo "Found stale PID file. Removing..."
            rm $PID_FILE
        fi
    fi

    echo -e "${GREEN}Starting $APP_NAME...${NC}"
    nohup $BIN_PATH >> $LOG_FILE 2>&1 &
    PID=$!
    echo $PID > $PID_FILE
    echo -e "${GREEN}Started with PID $PID. Logs are redirected to $LOG_FILE.${NC}"
}

stop() {
    if [ ! -f $PID_FILE ]; then
        echo -e "${RED}$APP_NAME is not running (PID file not found).${NC}"
        return
    fi

    PID=$(cat $PID_FILE)
    if ps -p $PID > /dev/null; then
        echo -e "${GREEN}Stopping $APP_NAME (PID: $PID)...${NC}"
        kill $PID
        rm $PID_FILE
        echo -e "${GREEN}Stopped.${NC}"
    else
        echo -e "${RED}Process $PID not found. Removing stale PID file.${NC}"
        rm $PID_FILE
    fi
}

restart() {
    stop
    sleep 1
    start
}

status() {
    if [ -f $PID_FILE ]; then
        PID=$(cat $PID_FILE)
        if ps -p $PID > /dev/null; then
            echo -e "${GREEN}$APP_NAME is running (PID: $PID).${NC}"
        else
            echo -e "${RED}$APP_NAME is not running (Stale PID file).${NC}"
        fi
    else
        echo "No PID file found."
    fi
}

case "$1" in
    build)
        build
        ;;
    start)
        build
        start
        ;;
    stop)
        stop
        ;;
    restart)
        restart
        ;;
    status)
        status
        ;;
    *)
        echo "Usage: $0 {build|start|stop|restart|status}"
        exit 1
        ;;
esac
