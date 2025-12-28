.PHONY: all build run start stop restart status log help clean

# 默认目标：显示帮助信息
all: help

# 赋予脚本执行权限 (Windows下如果使用Git Bash通常有效，CMD下可能忽略)
prepare:
	@chmod +x scripts/control.sh 2>/dev/null || true

# 编译项目
build: prepare
	@echo "正在编译..."
	@./scripts/control.sh build

# 运行 (前台模式) - 适合调试
run: prepare
	@echo "正在启动 (前台模式)..."
	@./scripts/control.sh run

# 启动 (后台模式)
start: prepare
	@echo "正在启动 (后台模式)..."
	@./scripts/control.sh start

# 停止服务
stop: prepare
	@echo "正在停止..."
	@./scripts/control.sh stop

# 重启服务
restart: prepare
	@echo "正在重启..."
	@./scripts/control.sh restart

# 查看服务状态
status: prepare
	@./scripts/control.sh status

# 查看实时日志
log:
	@echo "正在查看日志 (Ctrl+C 退出)..."
	@tail -f logs/server.log

# 打包发布 (需要传入版本号 v=x.y.z)
package:
	@./scripts/package.sh $(v)

# 显示帮助信息
help:
	@echo ""
	@echo "Animate Auto Tool Makefile 帮助"
	@echo "========================================"
	@echo "可用命令:"
	@echo "  make build    - 编译项目 (bin/animate-server)"
	@echo "  make run      - 以前台模式运行服务 (适合调试)"
	@echo "  make start    - 以后台模式启动服务"
	@echo "  make stop     - 停止后台服务"
	@echo "  make restart  - 重启服务"
	@echo "  make status   - 查看服务运行状态 (PID)"
	@echo "  make log      - 实时查看日志 (logs/server.log)"
	@echo "  make package  - 打包发布 (用法: make package v=1.0.0)"
	@echo "  make help     - 显示此帮助信息"
	@echo "========================================"
	@echo ""
