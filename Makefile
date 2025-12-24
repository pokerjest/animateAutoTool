.PHONY: all build start stop restart status log

# 默认动作
all: build

# 赋予脚本执行权限
prepare:
	chmod +x scripts/control.sh

build: prepare
	./scripts/control.sh build

start: prepare
	./scripts/control.sh start

stop: prepare
	./scripts/control.sh stop

restart: prepare
	./scripts/control.sh restart

status: prepare
	./scripts/control.sh status

# 方便查看日志
log:
	tail -f server.log

# 打包多平台发布版
package:
	./scripts/package.sh $(v)
