#!/bin/bash
# scripts/manage.sh

# Configuration
APP_NAME="animate-server"
BIN_DIR="./bin"
BIN_PATH="$BIN_DIR/$APP_NAME"
PID_FILE="$BIN_DIR/server.pid"
LOG_FILE="logs/server.log"
SRC_PATH="cmd/server/main.go"
SERVER_PORT=8306

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

mkdir -p $BIN_DIR
mkdir -p logs

function get_pid_by_port() {
    lsof -ti :$SERVER_PORT
}

function list_port_pids() {
    get_pid_by_port 2>/dev/null | awk 'NF {print $1}'
}

function kill_pids() {
    local signal="$1"
    shift
    local pids=("$@")

    for pid in "${pids[@]}"; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            if [ -n "$signal" ]; then
                kill "$signal" "$pid" 2>/dev/null
            else
                kill "$pid" 2>/dev/null
            fi
        fi
    done
}

function get_pid_from_file() {
    if [ -f "$PID_FILE" ]; then
        cat "$PID_FILE"
    fi
}

function check_deps() {
    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: 'go' is not installed.${NC}"
        exit 1
    fi
}

function build() {
    check_deps
    echo -e "${GREEN}Building $APP_NAME...${NC}"
    
    # Check if we should tidy first (optional, but good for stability)
    go mod tidy
    
    # Added CGO_ENABLED=0 based on control.sh logic for better portability
    CGO_ENABLED=1 go build -ldflags="-s -w" -o $BIN_PATH $SRC_PATH
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}Build successful.${NC}"
        return 0
    else
        echo -e "${RED}Build failed!${NC}"
        exit 1
    fi
}

function stop() {
    echo -e "${YELLOW}Stopping server...${NC}"
    
    # Method 1: PID File
    PID=$(get_pid_from_file)
    if [ -n "$PID" ]; then
        if kill -0 "$PID" 2>/dev/null; then
             echo "Killing process $PID (from PID file)..."
             kill "$PID"
        else
             echo "Process in PID file not running."
        fi
        rm "$PID_FILE"
    fi
    
    # Method 2: Port
    PORT_PIDS=($(list_port_pids))
    if [ ${#PORT_PIDS[@]} -gt 0 ]; then
        echo "Found process(es) ${PORT_PIDS[*]} listening on port $SERVER_PORT. Killing..."
        kill_pids "" "${PORT_PIDS[@]}"
    fi
    
    # Wait loop
    for i in {1..10}; do
        if [ -z "$(get_pid_by_port)" ]; then
            echo -e "${GREEN}Server stopped.${NC}"
            return
        fi
        sleep 0.5
    done
    
    # Force kill
    PID_FINAL=($(list_port_pids))
    if [ ${#PID_FINAL[@]} -gt 0 ]; then
        echo -e "${RED}Force killing PID(s) ${PID_FINAL[*]}...${NC}"
        kill_pids "-9" "${PID_FINAL[@]}"
    fi
}

function start() {
    # Ensure stopped
    if [ -n "$(list_port_pids)" ]; then
        echo -e "${YELLOW}Server seems to be running. Stopping first...${NC}"
        stop
    fi

    if [ -n "$(list_port_pids)" ]; then
        echo -e "${RED}Port $SERVER_PORT is still in use after stop. Aborting start.${NC}"
        exit 1
    fi

    echo -e "${GREEN}Starting $APP_NAME...${NC}"
    nohup $BIN_PATH >> $LOG_FILE 2>&1 &
    NEW_PID=$!
    echo $NEW_PID > $PID_FILE
    
    for i in {1..10}; do
        if kill -0 $NEW_PID 2>/dev/null; then
            PORT_PIDS=($(list_port_pids))
            for pid in "${PORT_PIDS[@]}"; do
                if [ "$pid" = "$NEW_PID" ]; then
                    echo -e "${GREEN}Server started with PID $NEW_PID.${NC}"
                    echo -e "Logs: ${YELLOW}$LOG_FILE${NC}"
                    return
                fi
            done
        else
            break
        fi
        sleep 0.5
    done

    rm -f "$PID_FILE"
    echo -e "${RED}Server failed to start. Check logs.${NC}"
    tail -n 20 $LOG_FILE
    exit 1
}

function status() {
    PID=$(get_pid_from_file)
    PID_PORT=($(list_port_pids))
    
    if [ ${#PID_PORT[@]} -gt 0 ]; then
        echo -e "${GREEN}$APP_NAME is running (PID: ${PID_PORT[*]}).${NC}"
    else
        if [ -n "$PID" ] && ! kill -0 "$PID" 2>/dev/null; then
            rm -f "$PID_FILE"
        fi
        echo -e "${YELLOW}$APP_NAME is stopped.${NC}"
    fi
}

function run() {
    # Foreground mode
    if [ -n "$(get_pid_by_port)" ]; then
        stop
    fi
    echo -e "${GREEN}Starting $APP_NAME in foreground...${NC}"
    $BIN_PATH
}

# Main Dispatch
CMD=$1
case $CMD in
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
        build
        stop
        start
        ;;
    run)
        build
        run
        ;;
    status)
        status
        ;;
    log)
        tail -f $LOG_FILE
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|build|run|status|log}"
        exit 1
        ;;
esac
