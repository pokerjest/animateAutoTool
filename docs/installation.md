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

## 1.0 升级前检查

官方支持从 `v0.9.9` 和 `v1.0.0-beta.*` 直接向前升级。`v0.6`～`v0.8` 不属于 1.0 的直接升级契约；这类数据库应先导出备份，再在隔离副本中验证恢复。

升级前必须：

1. 停止同一数据目录上的其他 AnimateTool 进程；
2. 在“备份”页创建加密 ZIP，并把副本放到应用数据目录之外；
3. 确认当前 `data/` 和 `config.yaml` 可读写；
4. 确认目标 Release 有当前系统和 CPU 架构的安装包、`SHA256SUMS.txt` 和 `animate-release-manifest.json`。

自动更新会先保存数据库和配置快照，并在新版本启动后执行本机 readiness 检查。版本回切不是数据库 downgrade；只有目标 manifest 明确允许时才会执行整套快照恢复。迁移失败或发现未知 schema 时，应用会拒绝启动业务服务，应该先查看日志和迁移快照，不要直接删除数据库。

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
