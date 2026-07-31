.PHONY: all frontend-build frontend-test build run start stop restart status log help clean doctor repair package package-e2e lint vulncheck

# 默认目标：显示帮助信息
all: help

# 赋予脚本执行权限
prepare:
	@chmod +x scripts/manage.sh 2>/dev/null || true

# 构建嵌入式 Vue 前端
frontend-build:
	@npm --prefix web/frontend ci
	@npm --prefix web/frontend run build

frontend-test:
	@npm --prefix web/frontend run test:run

# 编译项目
build: prepare
	@./scripts/manage.sh build

# 运行 (前台模式) - 适合调试
run: prepare
	@./scripts/manage.sh run

# 启动 (后台模式)
start: prepare
	@./scripts/manage.sh start

# 停止服务
stop: prepare
	@./scripts/manage.sh stop

# 重启服务
restart: prepare
	@./scripts/manage.sh restart

# 查看服务状态
status: prepare
	@./scripts/manage.sh status

# 查看实时日志
log:
	@echo "正在查看日志 (Ctrl+C 退出)..."
	@./scripts/manage.sh log

# 打包发布 (需要传入版本号 v=x.y.z)
package:
	@./scripts/package.sh $(v)

# 构建当前平台发行包，并对解压后的真实二进制执行无头端到端测试
package-e2e:
	@bash ./scripts/test-package-e2e.sh

# 运行 golangci-lint (使用项目固定的 .tools/bin/golangci-lint)
lint:
	@sh scripts/lint.sh

# 依赖漏洞扫描
vulncheck:
	@npm audit --prefix web/frontend --audit-level=moderate
	@go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

# 运行健康诊断
doctor:
	@go run ./cmd/doctor

# 执行下载日志与订阅修复
repair:
	@go run ./cmd/repair

# 清理本地构建与调试残留（保留 config.yaml 和运行数据）
clean:
	@bash ./scripts/clean_worktree.sh

# 显示帮助信息
help:
	@echo ""
	@echo "Animate Auto Tool Makefile 帮助"
	@echo "========================================"
	@echo "可用命令:"
	@echo "  make build    - 编译项目 (bin/AnimateAutoTool)"
	@echo "  make frontend-build - 构建嵌入式 Vue 前端"
	@echo "  make frontend-test  - 运行前端组件测试"
	@echo "  make lint     - 运行 golangci-lint 静态检查"
	@echo "  make vulncheck - 扫描前端与 Go 依赖已知漏洞"
	@echo "  make run      - 以前台模式运行服务 (适合调试)"
	@echo "  make start    - 以后台模式启动服务"
	@echo "  make stop     - 停止后台服务"
	@echo "  make restart  - 重启服务"
	@echo "  make status   - 查看服务运行状态 (PID)"
	@echo "  make log      - 实时查看按小时切分的服务日志"
	@echo "  make package  - 打包发布 (用法: make package v=1.0.0)"
	@echo "  make package-e2e - 打包当前平台并运行无头端到端验证"
	@echo "  make doctor   - 输出当前系统健康摘要"
	@echo "  make repair   - 执行一次下载日志与订阅修复"
	@echo "  make clean    - 清理本地构建、日志与调试残留"
	@echo "  make help     - 显示此帮助信息"
	@echo "========================================"
	@echo ""
