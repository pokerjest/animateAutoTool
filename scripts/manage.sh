#!/bin/bash
# scripts/manage.sh

# Configuration
APP_NAME="animate-server"
BIN_DIR="./bin"
BIN_PATH="$BIN_DIR/$APP_NAME"
PID_FILE="$BIN_DIR/server.pid"
LOG_DIR="logs"
LOG_PATTERN="server-*.log"
SRC_PATH="cmd/server/main.go"
SERVER_PORT=8306
VERSION_FILE="./VERSION"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

mkdir -p $BIN_DIR
mkdir -p "$LOG_DIR"

function latest_log_file() {
    find "$LOG_DIR" -maxdepth 1 -type f -name "$LOG_PATTERN" -print 2>/dev/null | sort | tail -n 1
}

function follow_logs() {
    local current_file=""
    local tail_pid=""

    function stop_log_tail() {
        if [ -n "$tail_pid" ]; then
            kill "$tail_pid" 2>/dev/null || true
            wait "$tail_pid" 2>/dev/null || true
        fi
    }

    trap 'stop_log_tail; exit 0' INT TERM
    trap 'stop_log_tail' EXIT

    while true; do
        local latest_file
        latest_file=$(latest_log_file)
        if [ -n "$latest_file" ] && [ "$latest_file" != "$current_file" ]; then
            stop_log_tail
            current_file="$latest_file"
            echo -e "${YELLOW}Following $current_file${NC}"
            tail -n 100 -f "$current_file" &
            tail_pid=$!
        fi
        sleep 2
    done
}

function get_pid_by_port() {
    # Only return the server that is listening on the configured port.
    # A broad `lsof -ti :PORT` also returns browser/client processes with an
    # established connection and can make status/stop target unrelated apps.
    lsof -tiTCP:$SERVER_PORT -sTCP:LISTEN
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
    if ! command -v npm &> /dev/null; then
        echo -e "${RED}Error: 'npm' is not installed (Node.js 22+ is required).${NC}"
        exit 1
    fi
}

function build() {
    check_deps
    echo -e "${GREEN}Building $APP_NAME...${NC}"

    npm --prefix web/frontend ci
    npm --prefix web/frontend run build

    BUILD_VERSION="dev"
    if [ -f "$VERSION_FILE" ]; then
        BUILD_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
    fi
    LD_FLAGS="-s -w -X github.com/pokerjest/animateAutoTool/internal/version.AppVersion=${BUILD_VERSION}"
    CGO_ENABLED=0 go build -ldflags="$LD_FLAGS" -o "$BIN_PATH" "$SRC_PATH"
    
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
    nohup "$BIN_PATH" >/dev/null 2>&1 &
    NEW_PID=$!
    echo $NEW_PID > $PID_FILE
    
    for i in {1..10}; do
        if kill -0 $NEW_PID 2>/dev/null; then
            PORT_PIDS=($(list_port_pids))
            for pid in "${PORT_PIDS[@]}"; do
                if [ "$pid" = "$NEW_PID" ]; then
                    echo -e "${GREEN}Server started with PID $NEW_PID.${NC}"
                    echo -e "Logs: ${YELLOW}$LOG_DIR/server-YYYYMMDD-HH.log${NC}"
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
    LATEST_LOG=$(latest_log_file)
    if [ -n "$LATEST_LOG" ]; then
        tail -n 20 "$LATEST_LOG"
    else
        echo -e "${YELLOW}No hourly server log was created.${NC}"
    fi
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
        follow_logs
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|build|run|status|log}"
        exit 1
        ;;
esac
