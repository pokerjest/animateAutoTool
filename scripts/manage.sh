#!/bin/bash
# scripts/manage.sh

# Configuration
SERVER_PORT=8306
BIN_DIR="./bin"
APP_NAME="animate-server"
BIN_PATH="$BIN_DIR/$APP_NAME"
LOG_FILE="logs/server.log"
SRC_PATH="cmd/server/main.go"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

mkdir -p $BIN_DIR
mkdir -p logs

function get_pid() {
    # Find PID by port using lsof (more reliable on Mac/Linux for port usage)
    lsof -ti :$SERVER_PORT
}

function check_pid_running() {
    if [ -z "$1" ]; then return 1; fi
    kill -0 $1 2>/dev/null
    return $?
}

function stop_server() {
    echo -e "${YELLOW}Stopping server on port $SERVER_PORT...${NC}"
    PID=$(get_pid)
    
    if [ -n "$PID" ]; then
        echo "Found running process PID: $PID. Sending SIGTERM..."
        kill $PID
        
        # Wait loop (max 5 seconds)
        for i in {1..10}; do
            if ! check_pid_running $PID; then
                echo -e "${GREEN}Server stopped successfully.${NC}"
                return
            fi
            sleep 0.5
        done
        
        # Force kill if still running
        echo -e "${RED}Server did not stop gracefully. Sending SIGKILL...${NC}"
        kill -9 $PID
        sleep 1
        
        # Final Verification
        if check_pid_running $PID; then
             echo -e "${RED}Failed to kill process $PID. Please check manually.${NC}"
             exit 1
        fi
        echo -e "${GREEN}Server stopped (forced).${NC}"
    else
        echo "No process found listening on port $SERVER_PORT."
    fi
}

function build_server() {
    echo -e "${GREEN}Building $APP_NAME...${NC}"
    
    # Check Go
    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: 'go' is not installed.${NC}"
        exit 1
    fi

    go mod tidy
    if go build -ldflags="-s -w" -o $BIN_PATH $SRC_PATH; then
        echo -e "${GREEN}Build successful.${NC}"
        return 0
    else
        echo -e "${RED}Build failed!${NC}"
        return 1
    fi
}

function start_server() {
    # Ensure port is free
    PID=$(get_pid)
    if [ -n "$PID" ]; then
        echo -e "${RED}Port $SERVER_PORT is still in use by PID $PID. Attempting to stop again...${NC}"
        stop_server
    fi
    
    echo -e "${GREEN}Starting $APP_NAME...${NC}"
    nohup $BIN_PATH >> $LOG_FILE 2>&1 &
    NEW_PID=$!
    
    # Wait a moment to check if it crashed immediately
    sleep 1
    if kill -0 $NEW_PID 2>/dev/null; then
        echo -e "${GREEN}Server started with PID $NEW_PID. Listening on port $SERVER_PORT.${NC}"
        echo -e "Logs: ${YELLOW}tail -f $LOG_FILE${NC}"
    else
        echo -e "${RED}Server failed to start immediately. Check logs:${NC}"
        cat $LOG_FILE | tail -n 10
        exit 1
    fi
}

function run_server() {
    # Foreground run
    if [ -n "$(get_pid)" ]; then
         stop_server
    fi
    echo -e "${GREEN}Starting $APP_NAME in foreground...${NC}"
    $BIN_PATH
}

# Main Logic
CMD=$1
case $CMD in
    start)
        build_server && stop_server && start_server
        ;;
    stop)
        stop_server
        ;;
    restart)
        build_server && stop_server && start_server
        ;;
    run)
        build_server && stop_server && run_server
        ;;
    build)
        build_server
        ;;
    status)
        PID=$(get_pid)
        if [ -n "$PID" ]; then
             echo -e "${GREEN}$APP_NAME is running (PID $PID) on port $SERVER_PORT${NC}"
        else
             echo -e "${YELLOW}$APP_NAME is stopped${NC}"
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|run|build|status}"
        exit 1
        ;;
esac
