# 安装与部署

## 发行包（推荐）

发行包包含服务程序、配置模板、启动脚本和必要的前端资源，不需要本地安装 Go。

| 系统 | 推荐资产 |
| --- | --- |
| Windows x64 | `AnimateAutoTool_*_windows_amd64.zip` 或独立 `.exe` |
| macOS Apple Silicon | `*_darwin_arm64.dmg` 或对应压缩包 |
| macOS Intel | `*_darwin_amd64.dmg` |
| Linux x64 | `*_linux_amd64.tar.gz` |

下载后固定目录、复制配置模板，再按[快速开始](getting-started.md)启动。

压缩包与 GitHub Release 资产使用 `AnimateAutoTool_*` 前缀；包内本地主程序名为 `AnimateAutoTool`（Windows 为 `AnimateAutoTool.exe`）。更新器仍兼容旧版 `animate-server_*` 资产。

## Docker Compose

仓库中的 `docker-compose.yml` 只提供起点。正式使用前必须确认三个挂载点：

```yaml
services:
  animate:
    volumes:
      - ./data:/app/data
      - ./config.yaml:/app/config.yaml
      - /你的媒体目录:/media
```

- `data` 保存 SQLite、图片缓存和本机状态；
- `config.yaml` 可能包含外部服务密钥；
- `/media` 必须与 qBittorrent 的实际下载目录保持一致。

容器内访问宿主机服务时，不要盲目使用 `localhost`；请使用 Compose 服务名或宿主机网关地址。

## 从源码构建

```bash
git clone https://github.com/pokerjest/animateAutoTool.git
cd animateAutoTool
./scripts/setup.sh
./scripts/manage.sh run
```

开发环境需要 Go 1.25+；前端资源变更时还需要 Node.js 22 和 `web/frontend/package-lock.json` 中锁定的依赖。

## 数据目录与权限

- 不要把真实 `config.yaml` 提交到 Git；
- Unix 下配置镜像会收紧为 `0600`；
- Windows 下请确保运行用户对配置和 `data/` 有读写权限；
- 新备份是 AES-256 加密 ZIP，但完整备份仍可能包含服务地址、Token 和 API Key；请使用强密码，并把文件副本保存在应用数据目录之外。

## 管理命令

| 目的 | 命令 |
| --- | --- |
| 前台运行 | `./scripts/manage.sh run` |
| 后台启动 | `./scripts/manage.sh start` |
| 停止 | `./scripts/manage.sh stop` |
| 重启 | `./scripts/manage.sh restart` |
| 查看状态 | `./scripts/manage.sh status` |
| 查看日志 | `./scripts/manage.sh log` |
| 本地密码恢复 | `http://localhost:8306/recover` |
