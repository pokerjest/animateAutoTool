# 🎬 Animate Auto Tool

<div align="center">

![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go)
![Version](https://img.shields.io/badge/Version-v0.2.0--Beta-blue?style=for-the-badge)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/pokerjest/animateAutoTool/go.yml?style=for-the-badge)

**一个优雅的动漫自动追番下载工具**

[功能特性](#-功能特性) • [快速开始](#-快速开始) • [使用说明](#-使用说明) • [开发](#-开发)

</div>

---

## 📖 简介

Animate Auto Tool 是一个基于 Go 开发的自动化动漫下载工具，通过订阅 [Mikan Project](https://mikanani.me/) 的 RSS 源，自动追踪您喜欢的动漫更新，并使用 qBittorrent 进行下载管理。

**核心优势**：
- 🎯 **智能订阅**：支持 Mikan RSS 订阅自动同步
- 🌸 **多源元数据**：聚合 Bangumi + TMDB + AniList 元数据，支持双向同步
- 🚀 **无人值守**：自动检测新番更新并下载
- 🎨 **现代 UI**：基于 HTMX + Tailwind 的玻璃拟态设计
- 📦 **单一二进制**：无需依赖，开箱即用
- 📂 **离线元数据**：自动缓存海报与简介，支持完全离线查看
- 📂 **本地管理**：智能扫描管理本地番剧库

---

## ✨ 功能特性

### 🌸 元数据深度集成 (持续增强!)
- **多源聚合**：可选 Bangumi, TMDB 或 AniList 作为首选元数据来源。
- **离线缓存**：所有海报和简介均以 BLOB 形式存储于 SQLite 中，**无网环境也能正常浏览**。
- **强制刷新**：单体/全局强制刷新元数据，确保信息实时同步。
- **双向同步**：自动同步 Bangumi 的"在看"和"看过"列表。
- **连接验证**：Bangumi 连接状态实时检测与修复。

### 📦 批量订阅
- **文本批量导入**：支持 `标题 | RSS链接` 格式的批量导入。
- **UI 交互添加**：在搜索或 Dashboard 中将番剧加入"批量列表"。
- **分组预览**：提交前可预览每个订阅包含的剧集详情，防止加错。

### 🛡️ 数据安全与备份
- **Cloudflare R2 备份**：支持将元数据、配置和订阅备份至 R2 对象存储。
- **选择性恢复**：从备份中按需恢复特定数据（如仅配置、仅订阅），灵活安全。

### 📂 本地番剧管理
- **目录扫描**：配置本地根目录，自动扫描识别番剧系列。
- **统一管理**：在 Web 界面查看本地已下载的番剧和文件统计。

### 核心功能
- ✅ **智能重命名**：根据规则自动重命名下载文件，整洁库房。
- ✅ **下载历史**：记录所有下载历史，避免重复下载。
- ✅ **连接测试**：一键测试 qBittorrent 连接状态；**R2 云备份读写验证**。
- ✅ **实时状态**：仪表盘实时显示订阅和下载状态。

### Web 界面
- 🎨 **现代化设计**：粉蓝配色，响应式布局
- ⚡ **无刷新交互**：HTMX 驱动的流畅体验
- 👁️ **密码管理**：密码显示/隐藏切换

---

## 🛠 技术栈

| 类型 | 技术 |
|------|------|
| **后端** | Go 1.24 + Gin + GORM |
| **数据库** | SQLite (Pure Go driver) |
| **前端** | HTMX + Alpine.js + Tailwind CSS |
| **下载器** | qBittorrent Web API |
| **RSS 源** | Mikan Project |
| **元数据源** | Bangumi / TMDB / AniList |
| **CI/CD** | GitHub Actions (Matrix Build) |

---

## 🚀 快速开始

### 前置要求

- Go 1.24+ (仅开发需要)
- qBittorrent 4.0+ (需开启 Web UI)
- SQLite 3

### 安装方式

#### 方式一：下载预编译二进制

从 [Releases](https://github.com/pokerjest/animateAutoTool/releases) 页面下载适合您系统的最新版本。

#### 方式二：从源码编译

```bash
# 克隆仓库
git clone https://github.com/pokerjest/animateAutoTool.git
cd animateAutoTool

# 编译 (CGO_ENABLED=0 即可使用纯 Go SQLite 驱动)
CGO_ENABLED=0 go build -ldflags="-s -w" -o animate-server cmd/server/main.go

# 运行
./animate-server
```

---

## ⚙️ 配置

### 1. 配置 qBittorrent

确保 qBittorrent 的 Web UI 已启用：

1. 打开 qBittorrent
2. 工具 → 选项 → Web UI
3. 勾选"启用 Web 用户界面"
4. 设置端口（默认 8080）和用户名/密码

### 2. 启动服务

```bash
./animate-server
```

服务默认运行在 `http://localhost:8306`

### 3. Web 界面配置

访问 `http://localhost:8306/settings` 配置：

- **qBittorrent Web UI 地址**：如 `http://localhost:8080`
- **用户名/密码**：qBittorrent 的 Web UI 凭据
- **下载保存目录**：下载文件的保存路径
- **元数据配置**：配置 TMDB API Token 或 AniList Token 以获取增强元数据

---

## 🔧 开发

### 项目结构

```
animateAutoTool/
├── cmd/
│   ├── server/          # 主服务器
├── internal/
│   ├── api/             # HTTP 路由和处理器
│   ├── db/              # 数据库初始化
│   ├── downloader/      # qBittorrent 客户端
│   ├── model/           # 数据模型
│   ├── parser/          # RSS 解析器
│   ├── scheduler/       # 定时任务调度
│   ├── service/         # 核心业务逻辑
│   ├── bangumi/         # Bangumi API 客户端
│   ├── tmdb/            # TMDB API 客户端
│   └── anilist/         # AniList API 客户端
├── web/
│   ├── templates/       # HTML 模板
│   └── static/          # 静态资源 (CSS/JS)
└── README.md
```

---

## 📝 待办事项

- [x] Mikan Dashboard 聚合页
- [x] 批量订阅功能
- [x] 本地番剧库管理
- [x] TMDB / AniList 增强元数据
- [x] 完全离线元数据缓存
- [ ] Docker 支持 (包含 qBittorrent 一体化镜像)
- [ ] 多用户支持
- [ ] 推送通知（WebSocket/Bark）
- [ ] 移动端适配优化
- [ ] 主题切换（深色模式）

---

## 🤝 致谢

- [Mikan Project](https://mikanani.me/) - RSS 订阅源
- [qBittorrent](https://www.qbittorrent.org/) - 下载客户端
- [HTMX](https://htmx.org/) - 现代化前端框架
- [Tailwind CSS](https://tailwindcss.com/) - CSS 框架
- [Bangumi](https://bgm.tv/) - 动漫元数据与同步

---

## 📄 许可证

本项目采用 MIT 许可证 - 详见 [LICENSE](LICENSE) 文件

---

## ⚠️ 免责声明

本工具仅供学习交流使用，请遵守当地法律法规，尊重版权。使用本工具产生的任何法律问题与作者无关。

---

<div align="center">

**如果觉得这个项目有帮助，请给个 ⭐️ Star 支持一下！**

Made with ❤️ by [pokerjest](https://github.com/pokerjest)

</div>

