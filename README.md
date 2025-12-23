# 🎬 Animate Auto Tool

<div align="center">

![Go Version](https://img.shields.io/badge/Go-1.23-00ADD8?style=for-the-badge&logo=go)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/pokerjest/animateAutoTool/go.yml?style=for-the-badge)

**一个优雅的动漫自动追番下载工具**

[功能特性](#-功能特性) • [快速开始](#-快速开始) • [使用说明](#-使用说明) • [开发](#-开发)

</div>

---

## 📖 简介

Animate Auto Tool 是一个基于 Go 开发的自动化动漫下载工具，通过订阅 [Mikan Project](https://mikanani.me/) 的 RSS 源，自动追踪您喜欢的动漫更新，并使用 qBittorrent 进行下载管理。

**核心优势**：
- 🎯 **智能订阅**：支持 RSS 订阅自动同步
- 🌸 **Mikan 聚合**：内置 Mikan Dashboard，新番一览无余
- 🚀 **无人值守**：自动检测新番更新并下载
- 🎨 **现代 UI**：基于 HTMX + Tailwind CSS 的响应式界面
- 📦 **单一二进制**：无需依赖，开箱即用
- 📂 **本地管理**：支持扫描和管理本地番剧库

---

## ✨ 功能特性

### 🌸 Mikan Dashboard (新!)
- **新番日历**：聚合显示每日更新的番剧，不错过每一部精彩。
- **历史回溯**：支持查看 2014-2025 年的季度番剧档案。
- **一键订阅**：在面板中直接点击番剧即可快速订阅。

### 📦 批量订阅 (新!)
- **文本批量导入**：支持 `标题 | RSS链接` 格式的批量导入。
- **UI 交互添加**：在搜索或 Dashboard 中将番剧加入"批量列表"。
- **分组预览**：提交前可预览每个订阅包含的剧集详情，防止加错。

### 📂 本地番剧管理 (新!)
- **目录扫描**：配置本地根目录，自动扫描识别番剧系列。
- **统一管理**：在 Web 界面查看本地已下载的番剧和文件统计。

### 核心功能
- ✅ **智能重命名**：根据规则自动重命名下载文件，整洁库房。
- ✅ **下载历史**：记录所有下载历史，避免重复下载。
- ✅ **连接测试**：一键测试 qBittorrent 连接状态。
- ✅ **实时状态**：仪表盘实时显示订阅和下载状态。

### Web 界面
- 🎨 **现代化设计**：粉蓝配色，响应式布局
- ⚡ **无刷新交互**：HTMX 驱动的流畅体验
- 👁️ **密码管理**：密码显示/隐藏切换

---

## 🛠 技术栈

| 类型 | 技术 |
|------|------|
| **后端** | Go 1.23 + Gin + GORM |
| **数据库** | SQLite |
| **前端** | HTMX + Alpine.js + Tailwind CSS |
| **下载器** | qBittorrent Web API |
| **RSS 源** | Mikan Project |
| **CI/CD** | GitHub Actions |

---

## 🚀 快速开始

### 前置要求

- Go 1.23+ (仅开发需要)
- qBittorrent 4.0+ (需开启 Web UI)
- SQLite 3

### 安装方式

#### 方式一：下载预编译二进制

从 [Releases](https://github.com/pokerjest/animateAutoTool/releases) 页面下载最新版本。

#### 方式二：从源码编译

```bash
# 克隆仓库
git clone https://github.com/pokerjest/animateAutoTool.git
cd animateAutoTool

# 编译
go build -o animate-server cmd/server/main.go

# 运行
./animate-server
```

#### 方式三：使用 Make

```bash
make build  # 编译
make run    # 运行
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

服务默认运行在 `http://localhost:8080`

### 3. Web 界面配置

访问 `http://localhost:8080/settings` 配置：

- **qBittorrent Web UI 地址**：如 `http://localhost:8080`
- **用户名/密码**：qBittorrent 的 Web UI 凭据
- **下载保存目录**：下载文件的保存路径

点击"测试连接"验证配置是否正确。

---

## 📚 使用说明

### 添加订阅

**方式一：Dashboard 添加 (推荐)**
1. 点击顶部的 **"🌸 Mikan"** 按钮。
2. 浏览每日更新或切换季度查看历史番剧。
3. 点击番剧封面，选择字幕组后添加。

**方式二：搜索添加**
1. 点击 **"🔍 搜索番剧"**。
2. 输入关键词，选择心仪的字幕组一键订阅。

**方式三：批量添加**
1. 点击 **"📦 批量添加"**。
2. 可手动粘贴 `标题 | RSS链接`，或在 Dashboard/搜索 中将番剧加入批量列表。
3. 点击 **"👀 预览内容"** 确认无误后提交。

### 自动下载

工具会定期检查订阅更新（默认每 10 分钟）：
- 发现新剧集 → 自动推送至 qBittorrent
- 记录下载历史 → 避免重复下载
- 根据规则重命名 → 方便管理

---

## 🔧 开发

### 项目结构

```
animateAutoTool/
├── cmd/
│   ├── server/          # 主服务器
│   └── debug_rss/       # RSS 调试工具
├── internal/
│   ├── api/             # HTTP 路由和处理器
│   ├── db/              # 数据库
│   ├── downloader/      # qBittorrent 客户端
│   ├── model/           # 数据模型
│   ├── parser/          # RSS 解析器
│   ├── scheduler/       # 调度器
│   └── service/         # 业务服务
├── web/
│   ├── templates/       # HTML 模板
│   └── static/          # 静态资源
└── README.md
```

### 开发命令

```bash
# 安装依赖
go mod download

# 运行测试
go test ./...

# 代码检查
go vet ./...

# 运行开发服务器
go run cmd/server/main.go

# 调试 RSS 解析
go run cmd/debug_rss/main.go
```

### 贡献指南

欢迎提交 Pull Request！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

---

## 📝 待办事项

- [x] Mikan Dashboard 聚合页
- [x] 批量订阅功能
- [x] 本地番剧库管理
- [ ] Docker 支持
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
