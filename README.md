# 🎬 Animate Auto Tool

<div align="center">

![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/pokerjest/animateAutoTool/go.yml?style=for-the-badge)

**一个面向自托管场景的动漫订阅、下载、整理与播放工作台**

[在线文档](https://pokerjest.github.io/animateAutoTool/) ·
[最新版本](https://github.com/pokerjest/animateAutoTool/releases/latest) ·
[问题反馈](https://github.com/pokerjest/animateAutoTool/issues)

</div>

## 项目简介

Animate Auto Tool 是运行在个人电脑、NAS 或服务器上的动漫自动化工具，把订阅、下载、整理和播放串成一条流程：

```text
Mikan RSS → 筛选订阅 → qBittorrent 下载 → 自动整理 → Jellyfin 播放
```

## 核心能力

- 订阅 Mikan RSS，按字幕组、清晰度、语言和规则筛选资源
- 调用 qBittorrent 下载、去重、移动并自动重命名文件
- 聚合 Bangumi、TMDB 和 AniList 元数据，构建本地媒体库
- 接入 Jellyfin 与 AList，支持多播放线路、继续观看和进度同步
- 提供 SQLite 存储、备份恢复、Cloudflare R2 云备份、日志与诊断
- 提供响应式 Web 界面，适合个人长期运行和家庭媒体库管理

## 快速启动

1. 从 [最新 Release](https://github.com/pokerjest/animateAutoTool/releases/latest) 下载对应系统的发行包。
2. 解压后复制 `config.yaml.example` 为 `config.yaml`。
3. Windows 双击 `start.bat`；macOS/Linux 执行 `./start.sh`。
4. 浏览器打开 `http://localhost:8306`，完成首次初始化。
5. 在设置页配置 qBittorrent，再添加第一条订阅。

Docker、源码构建、目录权限和升级方式请查看[安装与部署文档](https://pokerjest.github.io/animateAutoTool/installation/)。

## 文档

更完整的安装、外部 API 申请、媒体服务配置、备份、公网访问和 REST API 说明均维护在：

### [打开 Animate Auto Tool 在线文档](https://pokerjest.github.io/animateAutoTool/)

- [配置与外部 API](https://pokerjest.github.io/animateAutoTool/configuration/)
- [公网访问、DDNS 与反向代理](https://pokerjest.github.io/animateAutoTool/remote-access/)
- [故障排查](https://pokerjest.github.io/animateAutoTool/troubleshooting/)
- [REST API 与 OpenAPI](https://pokerjest.github.io/animateAutoTool/api/)

## 安全与贡献

首次初始化和密码恢复必须在本机完成。公网访问请使用 HTTPS、身份访问控制或 VPN，不要直接暴露路由器后台，也不要公开 `config.yaml`、备份文件或任何 API Key/Token。详细说明见[安全边界文档](https://pokerjest.github.io/animateAutoTool/first-run-security/)与 [`SECURITY.md`](SECURITY.md)。

欢迎提交 Issue 和 Pull Request。开发环境、测试和文档站维护方式见 [`CONTRIBUTING.md`](CONTRIBUTING.md)，版本变化见 [`CHANGELOG.md`](CHANGELOG.md)。

## 许可证

本项目采用 [MIT License](LICENSE)。
