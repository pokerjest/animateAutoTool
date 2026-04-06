# 🎬 Animate Auto Tool

<div align="center">

![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go)
![Version](https://img.shields.io/badge/Version-v0.4.4--Beta-blue?style=for-the-badge)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/pokerjest/animateAutoTool/go.yml?style=for-the-badge)

**一个优雅的动漫自动追番下载工具**

[功能特性](#-功能特性) • [快速开始](#-快速开始) • [使用说明](#-使用说明) • [开发](#-开发)

</div>

---

## 📖 简介

Animate Auto Tool 是一个基于 Go 1.25 开发的自动化动漫下载工具，专为追番党打造。它通过订阅 [Mikan Project](https://mikanani.me/) 的 RSS 源，自动追踪、下载并整理您喜欢的动漫，同时聚合 Bangumi、TMDB 和 AniList 的丰富元数据，为您呈现一个精美的本地番剧库。

**核心优势**：
- 🎯 **智能订阅**：一键订阅 Mikan RSS，自动同步更新。
- 🌸 **多源元数据**：聚合 Bangumi + TMDB + AniList，海报、简介一应俱全。
- 🚀 **自动化流程**：自动检测 -> 自动下载 -> 自动重命名 -> 自动整理。
- 🎨 **现代 UI**：基于 HTMX + Tailwind 的清新玻璃拟态界面。
- 📦 **开箱即用**：零依赖部署，内置 SQLite，无需复杂配置。
- 📂 **离线浏览**：元数据本地图片化存储，无网也能浏览精美海报。
- 📈 **运行时可观测**：内置运行时状态卡片与高负载提示色，适合单机长期稳定运行。

---

## ✨ 功能特性

### 🌸 极致的元数据体验
- **多源聚合**：灵活选择元数据源（Bangumi/TMDB/AniList），确保信息最全最准。
- **离线缓存**：所有图片和文本均存储于本地数据库，加载速度飞快，完全离线可用。
- **智能匹配**：自动解析种子文件名，精确匹配剧集信息。
- **双向同步**：支持同步 Bangumi 的"在看"进度。

### 📦 便捷的订阅管理
- **批量导入**：支持 `标题 | 链接` 格式的文本批量导入。
- **交互添加**：搜索结果一键添加，支持预览剧集列表，防止错订。
- **一览无余**：仪表盘实时展示今日更新和待看剧集。

### 🛡️ 数据安全与备份
- **三种备份模式**：支持全量备份、仅系统设置备份、以及仅导出 Cloudflare R2 云存档凭据。
- **Cloudflare R2 云备份**：支持把全量备份上传到 Cloudflare R2，并在新环境里只迁移 R2 连接配置。
- **按类型恢复**：全量/系统设置备份按选项恢复；Cloudflare-only 备份会合并恢复，不会误清空其他系统设置。

### 📂 强大的本地管理
- **文件扫描**：自动扫描本地目录，识别已下载的番剧文件。
- **智能重命名**：基于规则自动净化文件名，让文件夹整整齐齐。
- **下载历史**：去重机制确保不会重复下载同一集。

### 🔧 实用工具
- **连接测试**：内置 qBittorrent 和 R2 连接性自检工具。
- **日志监控**：Web 界面直接查看运行日志。
- **密码保护**：敏感信息（Token/密码）自动隐藏。
- **应用自更新**：支持检查 GitHub Release，并在 Windows `exe` / macOS `dmg` 形态下自动更新重启。
- **运行时状态**：设置页可查看 goroutines、Heap、GC、Uptime、CPU、GOMAXPROCS 等指标。
- **负载可视化**：对高 goroutine / 高内存 / 高频 GC 提供正常、关注、告警三档颜色提示。

---

## 🚀 快速开始

### 1. 前置要求

- **操作系统**: macOS / Linux / Windows (WSL)
- **Go 环境**: Go 1.25 或更高版本 (`go version` 检查)
- **版本文件**: 仓库根目录的 `VERSION` 是当前默认打包版本，`scripts/package.sh` 和 GitHub Actions 打包流程都会读取它
- **Linux 依赖**: `sudo apt-get install libgtk-3-dev libappindicator3-dev` (用于系统托盘)
- **下载器**: qBittorrent 4.0+ (需开启 Web UI)

### 2. 发行版使用指南 (推荐)

如果您直接下载了我们的预编译包（Recommended），请按照以下步骤操作：

#### Windows 用户
1. 解压下载的 `.zip` 压缩包。
2. 进入解压后的文件夹。
3. 找到 `config.yaml.example`，将其重命名为 `config.yaml`。
4. 使用记事本编辑 `config.yaml`，填入您的 qBittorrent 信息。
5. 双击 **`start.bat`** 即可启动服务（后台运行）。
   - 如需停止，请双击 `stop.bat`。
   - 如需前台调试，请双击 `run.bat`。

> [!NOTE]
> GitHub Actions 会同时产出带版本号的目录包、单文件 Windows `exe`，以及 macOS `dmg`，例如 `animate-server_v0.4.4_linux_amd64.tar.gz`、`animate-server_v0.4.4_windows_amd64.exe`、`animate-server_v0.4.4_darwin_arm64.dmg`。同时会产出 `SHA256SUMS.txt` 供应用内自动更新校验完整性使用。

#### macOS / Linux 用户
1. 解压下载的 `.tar.gz` 压缩包。
2. 在终端进入解压后的文件夹。
3. 重命名配置文件：`mv config.yaml.example config.yaml`
4. 编辑配置：`nano config.yaml` (填入 qBittorrent 信息)
5. 运行启动脚本：
   ```bash
   ./start.sh
   ```
   - 停止服务：`./stop.sh`
   - 重启服务：`./restart.sh`

### 3. 从源码编译安装

如果您希望从源码编译（开发者模式）：

```bash
# 克隆仓库
git clone https://github.com/pokerjest/animateAutoTool.git
cd animateAutoTool

# 步骤 1: 初始化环境 (检查依赖、创建目录、生成配置)
./scripts/setup.sh

# 步骤 2: 修改配置文件 (首次运行必须)
# 此时目录下已生成 config.yaml，请编辑填写必要的 qBittorrent 信息
vi config.yaml

# 步骤 3: 启动服务
./scripts/manage.sh start
```

**访问地址**: `http://localhost:8306`

> [!TIP]
> **中国大陆用户**：
> 编译过程如果遇到网络问题，请配置 GOPROXY：
> `export GOPROXY=https://goproxy.cn,direct`

---

## ⚙️ 详细配置

### 0. 公网部署前先理解安全边界

项目现在默认按下面的模型工作：

- **首次初始化阶段**：只允许在本机通过 `localhost` 直连访问。
- **初始化完成后**：才允许通过登录页进行公网访问。
- **忘记密码恢复**：使用本机专用的 `/recover` 页面，不对公网开放。
- **外部服务凭证**：qB / Jellyfin / AList / R2 等配置可以保存在本地，便于迁移和恢复。
- **应用登录密码**：保存为 bcrypt 哈希，不支持从数据库直接查看明文。

如果你准备通过反向代理暴露到公网，务必满足这几个条件：

- 只通过 HTTPS 对外提供访问。
- 正确设置 `server.public_url`。
- 只把你自己的反向代理 IP 或网段写进 `server.trusted_proxies`。
- 不要把 `0.0.0.0/0`、整段内网、或不受控代理加进受信任列表。

### 1. 初始化与本地恢复

首次启动时，系统会自动创建一个本地 bootstrap 管理员：

- 登录页会显示用户名，但**不会再在页面上显示随机密码**。
- 随机初始化密码会保存在本机数据目录的 `data/bootstrap/admin.json`。
- 完成首次改密后，这份 bootstrap 凭据会自动失效。

如果忘记登录密码：

- 请在**同一台机器**上通过 `http://localhost:8306/recover` 打开本地恢复页。
- `/recover` 只接受本机直连访问，不接受公网或反向代理转发访问。
- 重置成功后旧密码立即失效，可以直接回到 `/login` 用新密码登录。

### 2. qBittorrent 设置 (必须)
1. 打开 qBittorrent -> 设置 -> Web UI。
2. 勾选 **启用 Web 用户界面**。
3. 记下 **端口** (默认 8080) 和 **用户名/密码**。
4. **推荐**：取消勾选 "对本程序监听的 IP 地址和端口进行 CSRF 保护" 以避免连接问题。

### 3. 项目配置 (`config.yaml`)

```yaml
server:
  port: 8306
  mode: release # 或 debug
  public_url: "https://anime.example.com" # 公网访问时强烈建议填写
  trusted_proxies:
    - 127.0.0.1
    - ::1
    - 10.0.0.2 # 示例：你的 Nginx / Caddy / Traefik 所在机器 IP

database:
  path: data/animate.db

auth:
  secret_key: "replace-with-a-stable-random-secret"
```

说明：

- `public_url` 用于生成回调地址、同源校验和公网访问链接。
- `trusted_proxies` 只应填写你明确控制的反向代理 IP / CIDR。
- `auth.secret_key` 建议显式设置成稳定随机值；留空时程序会回退到 `data/bootstrap/auth_secret`。
- qB、Jellyfin、AList、R2 等业务配置仍然建议在 Web 设置页中填写和测试。

### 4. 反向代理建议

推荐让公网用户只访问反向代理，由反向代理转发到本机服务：

- 外网：`https://anime.example.com`
- 内部服务：`http://127.0.0.1:8306`

反向代理需要正确传递这些头：

- `Host`
- `X-Forwarded-Proto`
- `X-Forwarded-Host`
- `X-Forwarded-For`

如果这些头和 `public_url` / `trusted_proxies` 配置不一致，登录 Cookie、安全校验和同源保护都会受影响。

### 5. 备份与恢复
备份页现在支持三种模式：

- **全量备份**：导出整个数据库，适合完整迁移和灾难恢复。
- **系统设置备份**：只导出系统设置中的配置数据，适合迁移运行环境。
- **Cloudflare 云存档凭据**：当完整数据已经在 R2 时，只导出 R2 连接信息。

恢复时会先分析备份文件再展示可恢复内容：

- 全量备份通常包含配置、元数据、订阅、日志、本地库和用户信息。
- 系统设置备份通常只包含 `global_configs`。
- Cloudflare-only 备份会以“合并恢复”的方式写回当前设置，不会清空其他已有配置。

### 6. 高级配置 (Web 界面)
启动服务后，访问 `设置` 页面可配置：
- **元数据 API Token** (TMDB / AniList / Bangumi)
- **代理设置** (HTTP/SOCKS5)
- **备份设置** (Cloudflare R2)
- **运行时状态**（账户安全页签，登录后可见）

### 7. 运行时监控说明

项目已内置轻量运行时监控与前端可视化，默认用于单机/个人场景：

- **后端接口**：`GET /api/runtime/stats`（需要登录态）
- **前端入口**：设置页「账户安全」中的运行时状态卡片
- **监控指标**：`goroutines`、`heap_alloc`、`gc_count`、`gc/hour`、`uptime`、`num_cpu`、`gomaxprocs`
- **提示策略**：根据当前负载自动展示绿色（正常）/ 黄色（关注）/ 红色（告警）

说明：

- 该监控模块是轻量设计，不引入额外时序数据库，适合个人自用和低维护成本部署。
- 如需面向更高并发场景，建议后续接入 Prometheus/Grafana 与独立告警通道。

---

## 🛠 管理命令

项目根目录提供了一系列脚本方便管理：

| 命令 | 说明 | 对应脚本 |
|------|------|----------|
| `./scripts/start.sh` | 编译并后台启动服务 | `scripts/manage.sh start` |
| `./scripts/stop.sh` | 停止服务 | `scripts/manage.sh stop` |
| `./scripts/restart.sh` | 重启服务 | `scripts/manage.sh restart` |
| `./scripts/run.sh` | 前台运行服务 | `scripts/manage.sh run` |
| `./scripts/setup.sh` | 环境初始化 | - |
| `./scripts/package.sh $(cat VERSION)` | 生成当前版本打包包 | 读取 `VERSION` |
| `./scripts/manage.sh status` | 查看运行状态 | - |
| `./scripts/manage.sh log` | 实时查看日志 | - |
| `http://localhost:8306/recover` | 本机重置管理员密码 | 仅 localhost 可访问 |

---

## 🔧 开发指南

### 项目结构

```
animateAutoTool/
├── cmd/             # 主程序入口
├── internal/        # 核心业务代码
│   ├── api/         # HTTP 接口 (Gin)
│   ├── model/       # 数据模型 (GORM)
│   ├── service/     # 业务逻辑
│   ├── downloader/  # 下载器适配
│   └── ...
├── web/             # 前端资源
│   ├── templates/   # HTML 模板
│   └── static/      # CSS/JS
├── scripts/         # 运维脚本
└── config.yaml      # 配置文件
```

### 数据库迁移约定

项目现在使用显式 schema migration 机制，迁移入口在 [internal/db/migrations.go](./internal/db/migrations.go)。

- 启动时会先创建并维护 `schema_migrations` 表。
- 每条 migration 都会按顺序执行，并记录版本 ID、说明和应用时间。
- 当前数据库 schema 版本会写到启动日志里，便于排查升级状态。

以后做数据库演进时，建议按下面的规则执行：

- **新增表 / 新增字段**：追加新的 migration，不要修改已发布的旧 migration。
- **字段改名 / 类型变更 / 唯一约束收紧 / 历史数据搬迁**：必须写显式 migration，不要只改 model 然后依赖 `AutoMigrate`。
- **数据修复**：也放进 migration 或显式升级步骤，不要把一次性兼容逻辑散落进业务启动代码。
- **历史 migration 只追加不回改**：这样旧版本数据库才能稳定升级到新版本。

推荐流程：

1. 修改 GORM model。
2. 在 `internal/db/migrations.go` 追加新的 migration。
3. 为迁移补一个测试，至少覆盖“旧库升级到新版本”。
4. 运行 `go test ./internal/db -count=1` 和 `go test ./... -race`。

这套机制的目标不是替代 GORM，而是给未来版本迭代一个正式、可审计、可回归验证的数据库演进入口。

### 发布与移动端验收

为了把发版流程和真机检查固定下来，仓库里还补了两份可直接复用的清单：

- [docs/release-checklist.md](./docs/release-checklist.md)：发版前测试、打包、核心流程和 Release 检查项
- [docs/mobile-qa-checklist.md](./docs/mobile-qa-checklist.md)：手机端重点页面和弹窗的真机验收项

### 技术栈

| 领域 | 技术选型 |
|------|----------|
| **语言** | Go 1.25 |
| **Web 框架** | Gin |
| **ORM** | GORM |
| **数据库** | SQLite |
| **前端** | HTMX + Alpine.js + Tailwind CSS |
| **构建** | Bash Scripts + GitHub Actions |

---

## 🤝 致谢

- [Mikan Project](https://mikanani.me/) - 核心 RSS 数据源
- [qBittorrent](https://www.qbittorrent.org/) - 优秀的下载客户端
- [HTMX](https://htmx.org/) - 让前端回归简洁
- [Bangumi](https://bgm.tv/) - 丰富的动漫资料库

---

## 📄 许可证

本项目采用 [MIT License](LICENSE) 许可证。

---

<div align="center">

**Made with ❤️ by [pokerjest](https://github.com/pokerjest)**

</div>
