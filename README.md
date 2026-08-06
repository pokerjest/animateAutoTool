# 🎬 Animate Auto Tool

<div align="center">

<img src="docs/assets/mascot-icon.png" alt="AnimateTool" width="148">

![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/pokerjest/animateAutoTool/go.yml?style=for-the-badge)

**一个面向自托管场景的动漫订阅、下载、整理与播放工作台**

[在线文档](https://pokerjest.github.io/animateAutoTool/) ·
[最新版本](https://github.com/pokerjest/animateAutoTool/releases/latest) ·
[问题反馈](https://github.com/pokerjest/animateAutoTool/issues)

</div>

## 项目简介

Animate Auto Tool 是运行在个人电脑、NAS 或服务器上的动漫媒体工作台。它把追番订阅、本地媒体整理、元数据匹配和播放串成一条可检查、可修复的流程：

```text
Mikan RSS
→ 资源筛选与持久化对账
→ qBittorrent 下载
→ 下载后安全整理
→ 目标目录增量扫描
→ Bangumi / TMDB / AniList 元数据匹配
→ 本地播放或 Jellyfin 媒体模式
```

## 核心能力

- **追番与下载**：订阅 Mikan RSS，按字幕组、清晰度、语言和规则筛选；同步 qBittorrent 进度，并通过资源对账补交真正缺失的集数。
- **本地媒体整理**：识别季度、集数、多集范围和 SP/OVA 等特殊条目；下载完成后通过 qBittorrent 安全移动、重命名并定向增量扫描。
- **三源元数据**：并行查询 Bangumi、TMDB 和 AniList，保留真实来源 ID、字段来源和匹配证据。
- **双工作区播放**：管理模式维护订阅与媒体，媒体模式浏览 Jellyfin；支持 AnimateTool 代理、Jellyfin 直连、继续观看和进度同步。
- **可确认的 AI 协助**：支持 OpenAI、Gemini 和 Claude。AI 可以读取白名单内的诊断上下文并创建修复提案，写操作仍需在业务页面预览和确认。
- **运维与恢复**：提供健康监测、审计日志、诊断包、AES-256 加密 ZIP 备份、Cloudflare R2 云备份以及带兼容校验和失败恢复的更新器。
- **响应式 Web 界面**：适合个人长期运行，也可通过 HTTPS 反向代理、Cloudflare Access 或 VPN 安全访问。

## 适用范围

AnimateTool 适合已经使用或愿意使用 qBittorrent、希望长期维护动漫媒体库的自托管用户。Jellyfin、Cloudflare R2 和 AI 服务都是可选集成。

它不是 BT 客户端本身，也不是 Jellyfin 的完整替代品；当前本地扫描与整理重点面向番剧和电视剧。AI 不会仅凭聊天中的一句“处理一下”直接改数据库或移动文件。

## 1.0 发布与升级边界

当前仓库代码基线为 `v1.0.0` stable；以下升级边界和门禁是 1.0 版本必须满足的契约。

1.0 稳定版的官方直接升级来源是：

- `v0.9.9`
- `v1.0.0-beta.*`

`v0.6`～`v0.8` 的数据库只保留为非契约回归 fixture，不承诺直接升级。升级前请先创建加密 ZIP 备份，并把副本保存到应用数据目录之外。

数据库迁移是追加式协议：已发布 migration 的 ID、描述和 checksum 不得修改；遇到未知或更高 schema，旧程序会拒绝启动。回切不是数据库降级，只有 Release manifest 明确允许时，才会恢复“旧程序 + 数据库 + 配置”成套快照。

详细规则见[1.0 数据库与升级契约](https://pokerjest.github.io/animateAutoTool/release-1.0-migration-contract/)和[版本通道、更新与回切](https://pokerjest.github.io/animateAutoTool/usage/updater/)。

## 快速启动

1. 从 [最新 Release](https://github.com/pokerjest/animateAutoTool/releases/latest) 下载对应系统的发行包。
2. 解压后复制 `config.yaml.example` 为 `config.yaml`。
3. Windows 双击 `start.bat`；macOS/Linux 执行 `./start.sh`。
4. 浏览器打开 `http://localhost:8306`，完成首次初始化。
5. 在设置页配置 qBittorrent，再添加第一条订阅。需要媒体模式时，再配置 Jellyfin 地址和 API Key。

Docker、源码构建、目录权限和升级方式请查看[安装与部署文档](https://pokerjest.github.io/animateAutoTool/installation/)。

## 长期运行与故障定位

服务按小时写入 `logs/server-YYYYMMDD-HH.log`，异常事件单独写入 `logs/health-YYYYMMDD-HH.log`。遇到启动失败、数据库迁移异常、任务卡住或服务持续 5xx 时，优先导出“健康诊断包”；其中包含脱敏快照和 `goroutines.txt`，便于定位阻塞与泄漏。

不要把 `config.yaml`、数据库、备份文件、归档密码或未检查的诊断包发到公开 Issue。完整的日志字段、保留策略和排障顺序见[运行稳定性与故障定位](https://pokerjest.github.io/animateAutoTool/stability-observability/)。

## 文档

更完整的安装、外部 API 申请、媒体服务配置、备份、公网访问和 REST API 说明均维护在：

### [打开 Animate Auto Tool 在线文档](https://pokerjest.github.io/animateAutoTool/)

- [配置与外部 API](https://pokerjest.github.io/animateAutoTool/configuration/)
- [订阅、下载与资源对账](https://pokerjest.github.io/animateAutoTool/usage/subscriptions/)
- [本地媒体扫描与整理](https://pokerjest.github.io/animateAutoTool/usage/library/)
- [公网访问、DDNS 与反向代理](https://pokerjest.github.io/animateAutoTool/remote-access/)
- [故障排查](https://pokerjest.github.io/animateAutoTool/troubleshooting/)
- [REST API 与 OpenAPI](https://pokerjest.github.io/animateAutoTool/api/)

## 安全与贡献

首次初始化和密码恢复必须在本机完成。公网访问请使用 HTTPS、身份访问控制或 VPN，不要直接暴露路由器后台，也不要公开 `config.yaml`、备份文件或任何 API Key/Token。详细说明见[安全边界文档](https://pokerjest.github.io/animateAutoTool/first-run-security/)与 [`SECURITY.md`](SECURITY.md)。

欢迎提交 Issue 和 Pull Request。开发环境、测试和文档站维护方式见 [`CONTRIBUTING.md`](CONTRIBUTING.md)，版本变化见 [`CHANGELOG.md`](CHANGELOG.md)。

## 许可证

本项目采用 [MIT License](LICENSE)。
