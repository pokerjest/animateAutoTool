# Changelog

本项目所有显著变更都会记录在此文件。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/),版本号遵循 [SemVer](https://semver.org/lang/zh-CN/) 语义化版本。

发布说明的另一份完整列表见 [GitHub Releases](https://github.com/pokerjest/animateAutoTool/releases)。

## [Unreleased]

## [0.7.2] - 2026-07-23

### Fixed
- 修复代理地址未注入后台 Mikan 订阅、Bangumi 登录回调、部分观看进度、元数据图片、Jellyfin、AI 和更新请求的问题；新增按服务代理开关、地址校验与直接连通性测试。

## [0.7.1] - 2026-07-23

### Changed
- 首次在本机启动时可直接建立仅限 localhost 的初始化会话，由用户自行设置管理员密码，无需查找或输入随机密码。

### Security
- 初始化会话仅允许初始化未完成时通过 localhost 直连和同源请求建立；远程、代理转发及跨站请求继续被拒绝，完成初始化后入口自动关闭。

## [0.7.0] - 2026-07-23

### Added
- 恢复 Mikan 季度番组、文本搜索、字幕组选择、最近资源预览与 RSS 自动配置，并支持新建和编辑订阅时重新关联。
- 新增进程内任务注册表、任务快照 API 与类型化 `task_update` SSE，覆盖同步、扫描、订阅检查与修复、元数据、更新器和 R2 任务。
- 系统设置保存后同步写入本地 `config.yaml`，保留敏感字段留空不覆盖的行为。

### Changed
- 路由主内容加入约 200ms 的淡入上移过渡；异步按钮统一提供转圈、进行中文案、禁用状态和 `aria-busy`。
- 后台任务按钮持续显示到任务真正结束，断线或刷新后可通过任务快照恢复，并在完成后刷新对应数据。
- 追番日历通过海报进入条目并使用完整 Mikan 源添加订阅；备份、设置、AI、播放与认证流程统一异步反馈。

### Fixed
- 修复海报加载失败、Mikan ID 与 Bangumi subject ID 混用，以及旧订阅缺失 Mikan ID 的兼容回填问题。
- 修复列表操作共享忙碌状态、重复提交、后台请求已接收后按钮过早停止和减少动态效果时仍旋转的问题。

## [0.6.1] - 2026-07-23

### Fixed
- Fixed release CI lint failures so tests, lint, and cross-platform packaging pass.

### Security
- Upgraded the Go toolchain and dependencies with known vulnerabilities.

## [0.6.0] - 2026-07-23

### Added
- 使用 Vue 3、TypeScript、Vue Router、Pinia、TanStack Vue Query、Tailwind CSS、Reka UI 与 Lucide 重建完整前端。
- 新增 `/api/v1` JSON API、OpenAPI 3.1 契约、生成式 TypeScript 类型、统一响应和错误结构。
- 新增类型化 SSE 全局任务中心，覆盖扫描、订阅、元数据、下载、备份等长任务。
- 完整覆盖登录、初始化、恢复、仪表盘、订阅、日历、媒体库、本地番剧、播放、备份、健康、设置和 AI 页面。
- R2 上传与暂存改为异步任务，支持进度展示、连通性测试与选择性恢复。
- `CONTRIBUTING.md`、Issue / PR 模板、本 `CHANGELOG.md`,完善社区贡献流程。
- 重写 `SECURITY.md`,明确支持的版本范围与漏洞报告流程。
- `docs/api.md`:全量 HTTP 路由参考文档,按功能分组列出所有 API。
- 审计日志:新增 `audit_logs` 表与 `004_audit_logs` migration,记录登录、密码变更、删除订阅 / 本地目录、备份恢复、R2 删除、AI 设置变更等敏感操作。新增 `GET /api/audit-logs` 端点查询。
- `UserStore`:收口登录、改密、bootstrap 认证和当前会话用户读取的用户表访问。
- 补 `internal/launcher`(URL helpers、unzip / untar 路径穿越防护)、`internal/service`(backup_profiles 纯函数)、`internal/api`(`truncateChatHistory`、登出 / 改密 / 状态端点)的测试,`launcher` 覆盖率从 20.0% 提升到 31.4%,`api` 从 30.2% 提升到 32.7%。

### Fixed
- 修复 `/api/v1/setup/bootstrap` 被首次初始化中间件误拦截的问题。
- 阻止浏览器将登录凭据自动填入 R2 等设置字段。
- 补齐移动端主题切换、退出登录、焦点管理和无横向溢出的响应式布局。
- 前端将 AI 回复、Bangumi / AniList 简介、Toast、R2 进度错误等动态文本改为转义或文本渲染,降低 XSS 风险。
- AI 助手聊天历史改为按用户会话隔离,避免多用户部署时串上下文。
- 修正 `SECURITY.md` 对外部服务凭据“加密存储”的不准确描述,明确当前依赖本机文件权限与 Web 脱敏。

### Changed
- 生产环境移除 Go HTML 模板、HTMX、Alpine、浏览器端 Tailwind 编译器和远程 CDN 运行时依赖。
- Go embed 改为直接嵌入 `web/dist`，继续提供可离线运行的自托管单二进制。
- CI、Docker、Makefile 与发布脚本统一先构建 Node 22 前端，再构建 Go 服务。
- 收紧 CSP，并保留 Cookie 会话、同源写保护、本机恢复限制和现有 SQLite/配置格式。

### Internal
- `internal/model.AuditLog`、`internal/store.AuditLogStore`、`internal/service.RecordAudit` 形成审计日志的分层落地;handler 调用一致采用 `buildAuditContext(c)` 注入会话上下文。

## [0.5.4] - 2026-04-30

### Added
- `SECURITY.md` 首次加入仓库。
- AI 模块与 store 扩展的测试覆盖;补充 AI / Windows 文档。

### Changed
- 加固 Windows 启动流程,优化外部连接稳定性。
- 加固 updater 进度上报与 release 资产校验。

## [0.5.3.2] - 2026-04-29

### Added
- AI 工具页面与配套接口。

### Fixed
- 修复 health / 设置页面回归问题。

## [0.5.3.1] - 2026-04-29

### Fixed
- 修复 v0.5.3 引入的 CI lint 回归。
- 清理 v0.5.3 lint 修复后遗留的未使用辅助函数。

## [0.5.3] - 2026-04-29

### Added
- `docs/architecture.md` 架构指南,正式说明分层与 store 约定。
- `DownloadLog` / `LocalAnime` / `AnimeMetadata` store 与对应测试。
- 订阅策略、健康诊断 / doctor / repair 工具与配套 store helper。
- 覆盖 parser / launcher / updater 的纯函数测试。

### Changed
- 把 API 调用统一收口到 store 层,移除散落的 `db.DB.Where(...)` 直连。
- 收紧访问边界,扩大 store 测试覆盖。

### Chore
- 忽略 `.claude/` 项目元数据目录。

## [0.5.2] - 2026-04-29

### Changed
- 准备 v0.5.2 发布,加固运行时集成。
- 修复 v0.5.2 最后阻塞发布的 lint 问题。

## [0.5.1] - 2026-04-29

### Changed
- 加固 service 层边界。

## [0.5.0] - 2026-04-29

### Added
- 稳定化应用主流程的多处改动。

### Changed
- 工具链升级至 Go 1.25.9。
- 修复多轮 CI lint 回归。

## [0.4.11] - 2026-04-07

### Changed
- 文档与打包默认值同步到 v0.4.11。
- 修复 Windows 发布版本控制台残留,日志改为写入文件。

### Added
- 目录选择器;修正默认下载路径处理。

## [0.4.0] - 2026-04-03

首个对外正式发布版本之一,确立预编译多平台分发流程(Linux / Windows / macOS × amd64 / arm64)。

## [0.3.0] - 2025-12-30

早期里程碑版本。

---

[Unreleased]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.2...HEAD
[0.7.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.4...v0.6.0
[0.5.4]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.3.2...v0.5.4
[0.5.3.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.3.1...v0.5.3.2
[0.5.3.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.3...v0.5.3.1
[0.5.3]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.4.11...v0.5.0
[0.4.11]: https://github.com/pokerjest/animateAutoTool/compare/v0.4.0...v0.4.11
[0.4.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/pokerjest/animateAutoTool/releases/tag/v0.3.0
