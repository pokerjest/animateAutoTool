# 🎬 Animate Auto Tool

<div align="center">

![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go)
![Version](https://img.shields.io/badge/Version-v0.4.0--Beta-blue?style=for-the-badge)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/pokerjest/animateAutoTool/go.yml?style=for-the-badge)

**一个优雅的动漫自动追番下载工具**

[功能特性](#-功能特性) • [快速开始](#-快速开始) • [使用说明](#-使用说明) • [开发](#-开发)

</div>

---

## 📖 简介

Animate Auto Tool 是一个基于 Go 1.24 开发的自动化动漫下载工具，专为追番党打造。它通过订阅 [Mikan Project](https://mikanani.me/) 的 RSS 源，自动追踪、下载并整理您喜欢的动漫，同时聚合 Bangumi、TMDB 和 AniList 的丰富元数据，为您呈现一个精美的本地番剧库。

**核心优势**：
- 🎯 **智能订阅**：一键订阅 Mikan RSS，自动同步更新。
- 🌸 **多源元数据**：聚合 Bangumi + TMDB + AniList，海报、简介一应俱全。
- 🚀 **自动化流程**：自动检测 -> 自动下载 -> 自动重命名 -> 自动整理。
- 🎨 **现代 UI**：基于 HTMX + Tailwind 的清新玻璃拟态界面。
- 📦 **开箱即用**：零依赖部署，内置 SQLite，无需复杂配置。
- 📂 **离线浏览**：元数据本地图片化存储，无网也能浏览精美海报。

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
- **Cloudflare R2 备份**：支持将元数据、配置和订阅列表加密备份至云端。
- **选择性恢复**：灵活恢复所需数据（如仅恢复配置或仅恢复订阅），拒绝"一刀切"覆盖。

### 📂 强大的本地管理
- **文件扫描**：自动扫描本地目录，识别已下载的番剧文件。
- **智能重命名**：基于规则自动净化文件名，让文件夹整整齐齐。
- **下载历史**：去重机制确保不会重复下载同一集。

### 🔧 实用工具
- **连接测试**：内置 qBittorrent 和 R2 连接性自检工具。
- **日志监控**：Web 界面直接查看运行日志。
- **密码保护**：敏感信息（Token/密码）自动隐藏。

---

## 🚀 快速开始

### 1. 前置要求

- **操作系统**: macOS / Linux / Windows (WSL)
- **Go 环境**: Go 1.24 或更高版本 (`go version` 检查)
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
./start.sh
```

**访问地址**: `http://localhost:8306`

> [!TIP]
> **中国大陆用户**：
> 编译过程如果遇到网络问题，请配置 GOPROXY：
> `export GOPROXY=https://goproxy.cn,direct`

---

## ⚙️ 详细配置

### 1. qBittorrent 设置 (必须)
1. 打开 qBittorrent -> 设置 -> Web UI。
2. 勾选 **启用 Web 用户界面**。
3. 记下 **端口** (默认 8080) 和 **用户名/密码**。
4. **推荐**：取消勾选 "对本程序监听的 IP 地址和端口进行 CSRF 保护" 以避免连接问题。

### 2. 项目配置 (`config.yaml`)

```yaml
server:
  port: 8306
  mode: debug # 或 release

database:
  path: data/animate.db

# 关键配置：请填入您的真实信息
qbittorrent:
  url: "http://localhost:8080"
  username: "admin"
  password: "your_password"
  save_path: "/downloads/anime" # 下载保存路径
```

### 3. 高级配置 (Web 界面)
启动服务后，访问 `设置` 页面可配置：
- **元数据 API Token** (TMDB / AniList / Bangumi)
- **代理设置** (HTTP/SOCKS5)
- **备份设置** (Cloudflare R2)

---

## 🛠 管理命令

项目根目录提供了一系列脚本方便管理：

| 命令 | 说明 | 对应脚本 |
|------|------|----------|
| `./start.sh` | 编译并后台启动服务 | `scripts/manage.sh start` |
| `./stop.sh` | 停止服务 | `scripts/manage.sh stop` |
| `./restart.sh` | 重启服务 | `scripts/manage.sh restart` |
| `./scripts/setup.sh` | 环境初始化 | - |
| `./scripts/manage.sh status` | 查看运行状态 | - |
| `./scripts/manage.sh log` | 实时查看日志 | - |

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

### 技术栈

| 领域 | 技术选型 |
|------|----------|
| **语言** | Go 1.24 |
| **Web 框架** | Gin |
| **ORM** | GORM |
| **数据库** | SQLite |
| **前端** | HTMX + Alpine.js + Tailwind CSS |
| **构建** | Make / Bash Scrips |

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
