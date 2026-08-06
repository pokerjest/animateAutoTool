# 架构指南

本文记录 Animate Auto Tool 的关键架构约定。新增功能或重构时请优先沿用，避免再次走回散落直连数据库的老路。

## 目录全景

```
cmd/
├── server/           # 主服务
├── doctor/           # 离线体检 CLI（只读）
└── repair/           # 离线修复 CLI（写库，支持 --dry-run）
internal/
├── api/              # Gin handler + 中间件 + view layer
├── service/          # 业务逻辑 + worker 入口 + access helper
├── store/            # 数据访问层（每个领域一个 *Store）
├── model/            # GORM model 定义
├── db/               # 连接初始化 + 显式 migration
├── httpx/            # 统一的 HTTP client 工厂
├── parser/           # RSS / 文件名 / 标题解析
├── downloader/       # qBittorrent 适配
├── anilist, bangumi, jellyfin, tmdb/          # 当前外部服务适配
├── alist/            # 旧 AList 兼容适配，不属于当前前端能力
├── launcher/         # 外部服务子进程托管与兼容入口
├── updater/          # 应用自更新（GitHub Release）
├── scheduler, worker, event/                  # 定时任务 + 事件总线
├── config, security, safeio, bootstrap, version, tray, renamer/
└── ...
web/                  # Vue 前端源码 + Vite 产物 + embed.go
scripts/              # 部署/打包脚本
docs/                 # MkDocs 文档源文件（配置、API、公网访问、QA 清单）
mkdocs.yml            # 文档站导航、主题和严格构建配置
requirements-docs.txt # 固定的文档构建依赖
build/docs-site/      # MkDocs 生成目录（不提交）
```

## 数据访问：Store + Access Helper

**原则**：新增或重写的 handler / service 不直接调用 `db.DB.Where(...).Find(...)`。业务 SQL 应优先路过 `internal/store/` 中的 *Store 类型。

迁移状态：订阅、下载日志、本地库、元数据、配置、审计日志和用户认证链路已经有 store 层；部分历史 handler / service 仍存在直接 `db.DB` 调用。碰到这些代码时，优先随手收口到对应 store，不要继续扩大直连面。

### Store 层（`internal/store/`）

每个领域一个 store：
- `SubscriptionStore` — 订阅与运行状态
- `DownloadLogStore` — 下载日志
- `LocalAnimeStore` — 本地番剧目录 / 番剧 / 单集
- `AnimeMetadataStore` — 元数据 + 跨表 propagate
- `ConfigStore` — `global_configs` 键值
- `AuditLogStore` — 敏感操作审计日志
- `UserStore` — 登录用户、密码哈希与 bootstrap 认证

约定：
- 第一个参数固定 `*gorm.DB`，构造函数 `NewXxxStore(db)`。
- 每个方法**第一行必检** `if s == nil || s.db == nil { return ..., gorm.ErrInvalidDB }` —— 调用方再不需要 nil guard。
- 返回**指针**：`(*model.X, error)`，缺失统一 `gorm.ErrRecordNotFound`。
- 列表方法返回 `([]model.X, error)`；空入参短路返回 `(nil, nil)` 而不是错误。
- 复杂事务封装在 store 内（例：`LocalAnimeStore.RemoveDirectoryWithAnimes`），调用方不写 `db.DB.Begin()`。

### Access Helper（`internal/api/*_access.go`、`internal/service/*_access.go`）

每个使用 store 的包都有一个 access helper 文件，例：

```go
// internal/api/subscription_access.go
func subscriptionStore() *store.SubscriptionStore {
    if db.DB == nil { return nil }
    return store.NewSubscriptionStore(db.DB)
}
func subscriptionByID(id any) (*model.Subscription, error) {
    s := subscriptionStore()
    if s == nil { return nil, gorm.ErrInvalidDB }
    return s.GetByID(id)
}
```

**为什么不直接在 handler 里 `store.NewXxxStore(db.DB)`**：
- 消除 `db.DB == nil` 检查的样板代码
- 测试时可以直接替换 helper（虽然目前没有这样做，但保留口子）
- 包内统一入口，grep 一处即可知所有 handler 用了哪些 store

### configValue() 模式

任何对 `global_configs` 的读取，**禁止** `db.DB.Where("key = ?", ...).First(...)`，统一走 `configValue(key)`：

```go
// service 与 api 各有一份，签名相同
func configValue(key string) string {
    if db.DB == nil { return "" }
    return store.NewConfigStore(db.DB).GetDefault(key, "")
}
```

`metadata_service.initClients` 是这套模式的最佳示例：原本 40+ 行重复的 `db.DB.Where("key = ?", ...).First(&cfg)` 缩成 6 行。

## HTTP 客户端：`internal/httpx`

**所有外部 HTTP 调用**（Bangumi / TMDB / AniList / Jellyfin / qBittorrent / Mikan，以及仍保留的兼容适配器）统一通过 `httpx.NewRestyClient` 或同一包提供的标准客户端创建：

```go
client := httpx.NewRestyClient(timeout, proxyURL, headers)
resp, err := httpx.NewRequest(ctx, client).Get(url)
```

约定：
- 每个外部 client 都暴露**两套方法**：`Foo()` 和 `FooContext(ctx)`，前者只是 `FooContext(context.Background())` 的便捷包装。新代码请走 ctx 版本。
- 超时由调用方传入，不要在 httpx 里写死默认值。
- proxy / UA / headers 通过参数注入；代理地址统一由 `httpx.NormalizeProxyURL` 校验和规范化，业务入口按服务开关注入，不读取系统环境代理。
- `httpx.NewHTTPClient` / `NewHTTPClientWithProxy` 同样基于 `newHTTPTransport`，**显式禁用环境变量代理**并强制 30s connect / 30s keep-alive，避免被系统 `HTTP_PROXY` 影响。

## AI 能力：`internal/ai` 与工具执行器

AI 层支持三类提供商：

- OpenAI Chat Completions；
- Gemini 原生 `generateContent` 或 OpenAI-compatible；
- Claude 原生 Messages API 或 OpenAI-compatible。

各供应商通过 `CompletionClient` 归一化聊天、模型列表、工具调用和错误信息。原生客户端负责协议转换，OpenAI-compatible 网关复用标准客户端；业务层不应自行拼供应商 HTTP 请求。

内部工具注册表把能力分为：

```go
const (
    ToolRiskRead    ToolRisk = "read"
    ToolRiskPropose ToolRisk = "propose"
    ToolRiskWrite   ToolRisk = "write"
)
```

- `read` 只读取健康、订阅、日志、本地库和真实元数据候选；
- `propose` 生成结构化预览或持久化提案，不直接修改业务数据；
- `write` 应用固定提案，必须要求确认。

只有 `read` 和 `propose` 会出现在模型可见的工具定义中。`write` 工具由后端在用户完成业务页预览和确认后调用，不能让模型在第二轮重新生成执行参数。

写操作链路固定为：

```text
只读工具收集上下文
→ AI 生成提案
→ JSON Schema、权限和目标状态校验
→ 页面展示差异
→ 用户确认并获取一次性令牌
→ 使用服务器保存的 apply_tool 与参数执行
→ 记录 AIToolRun 和业务审计日志
```

确认令牌绑定用户、提案、目标、输入指纹、执行工具、有效期和 nonce。跨用户、跨提案、重复使用、文件或数据库状态变化都必须拒绝。

约定：

- 所有工具参数拒绝未知字段，不允许客户端覆盖用户 ID；
- AI 不直接执行 SQL、Shell、任意 URL 或任意文件系统操作；
- 元数据匹配只能选择确定性搜索真实返回的候选 ID；
- 文件整理仍经过目录边界、冲突、指纹和 qBittorrent 做种保护；
- 日志记录脱敏后的参数摘要和哈希，不记录 API Key、Cookie、认证头或完整模型原始响应；
- LLM 不可用、超时或限流时，确定性订阅、扫描、匹配、健康和备份功能必须继续工作；
- 数据库 schema 迁移永远不由 AI 决策或执行。

## 数据库迁移：`internal/db/migrations.go`

显式 schema migration，**只追加不修改**：

```go
var migrations = []migration{
    {ID: "001_initial_schema", Apply: ...},
    // ...
    {ID: "014_anime_metadata_extended_fields", Apply: ...},
    {ID: "015_local_anime_identity", Apply: ...},
}
```

规则：

- migration ID 必须以稳定三位数序号开头，启动时验证顺序和重复；
- **新增字段 / 新表** → `tx.AutoMigrate(&model.X{})` 即可
- **改列名 / 改类型 / 收紧约束 / 数据搬迁** → 必须**新写一条 migration**，不要修改老的
- 启动时 `schema_migrations` 表记录 ID、数值序号、描述和应用时间，`CurrentSchemaVersion` 按数值序号确定当前版本；
- 文件数据库迁移使用跨进程锁，阻止两个服务同时修改 schema；
- migration 前创建带 SHA256 的安全快照；官方升级矩阵固定覆盖 `v0.9.9`、`v1.0.0-beta.1`、`v1.0.0-beta.7`、`v1.0.0-beta.14` 到当前 schema；`v0.6`～`v0.8` 仅作为非契约回归数据（若 fixture 存在）；
- `schema_migrations` 保存稳定 fingerprint/checksum；历史 migration 被改写、数据库声明未来 schema 或存在未知 migration 时，应用拒绝启动；
- 每次迁移运行写入 `data/updates/migration-runs/current.json`，009/015 破坏性修复写入带映射和统计的审计报告；
- 数据修复脚本也**走 migration**，不要散落进业务启动代码

## HTTP 路由与中间件

入口在 `internal/api/routes_embedded.go`，资源用 `embed.FS` 嵌入二进制。

海报原图保存在 SQLite，`GET /api/v1/posters/{id}?width=...&v=...` 按需生成缩略图并放入有界内存缓存；内容指纹作为 `ETag`，版本化 URL 使用浏览器私有 immutable 缓存。缩略图生成限制并发，避免移动端首次请求大量海报时把服务端内存推高。Vite 哈希资源使用一年 immutable 缓存。

中间件分层（已在 routes_embedded 装配）：
1. `SecurityHeadersMiddleware` — CSP / XFO / Permissions-Policy 等浏览器侧硬约束
2. `BootstrapLocalOnlyMiddleware` — 首次初始化前**只允许 localhost 直连**
3. `SameOriginMiddleware` — 任何写操作都要求同源（避免 CSRF）
4. `AuthMiddleware` — 会话校验，未登录 401 / 重定向 `/login`
5. `DirectLocalOnlyMiddleware` — 仅用于 `/recover`，强制 loopback + 拒绝 forwarded headers

完整的 `/api/v1` 路由契约维护在 [`openapi.yaml`](openapi.yaml)，用户可读的认证、示例和安全说明见 [`api.md`](api.md)。

## 配置与安全

- 业务密码存 bcrypt，bootstrap admin 初始密码写 `data/bootstrap/admin.json`，但不会下发给浏览器；首次启动可通过仅限 localhost 的 `/api/v1/session/bootstrap` 建立初始化会话，首次改密后凭据与入口同时失效
- `auth.secret_key` 留空会自动落到 `data/bootstrap/auth_secret`（不要提交到仓库）
- `server.trusted_proxies` 只填明确控制的反向代理 IP / CIDR
- `server.public_url` 用于生成回调地址 + 同源校验
- 所有外部凭据（qB / Jellyfin / R2 / TMDB / AniList / Bangumi）运行时保存在 `global_configs` 表并通过 `configValue` 读取，同时镜像到本机 `config.yaml` 的 `system_settings`；应用登录密码不进入 YAML

## 日志

- 主进程 `cmd/server/main.go` 的 `configureLogging` 把 stderr / stdout 写到按本地时间分时的 `logs/server-YYYYMMDD-HH.log`
- `RequestLoggingMiddleware` 记录 request ID、method、route、status、耗时和错误数量，不记录 query、body、cookie 或 authorization；慢请求和 HTTP 5xx 同时进入 health 日志
- 后台任务、调度器、事件总线、更新器、备份恢复和托管服务在失败时记录阶段、对象 ID、恢复动作和 panic 堆栈
- 健康诊断导出包含 `goroutines.txt`、失败任务、数据库/schema 快照和脱敏异常日志
- 运行跨过整点后，首条新日志自动切换到新的小时文件；最多保留最近 168 个小时文件（7 天）
- 旧版本留下的 `logs/server.log` 不会被自动删除，升级后可按需手动归档
- 不引入 lumberjack 等第三方依赖

## 自更新（`internal/updater`）

- 拆分为 `manager.go` / `manager_apply.go` / `manager_release_assets.go` / `manager_versions.go`
- Release asset 命名 `<binary>_<version>_<goos>_<goarch>.<ext>`，配合 `SHA256SUMS.txt` 和 `animate-release-manifest.json`
- 兼容清单声明版本通道、目标 schema、最低升级版本、可读 schema 范围、测试版回切和回滚能力
- 自动更新只向前；手动回切只允许从测试版进入清单明确支持的稳定版
- 校验流程：拉 release → 读取兼容清单 → 判断版本/schema → 选择平台资产 → 下载 → 比对 SHA256 → 应用
- 应用前通过 SQLite `VACUUM INTO` 创建数据库快照，并复制 `config.yaml`、记录 SHA256 和 manifest
- 新版本启动后必须通过仅本机 readiness 检查；失败时辅助进程恢复旧程序、数据库和配置
- 快照默认保留最近 5 份或 30 天内的有效项，取更严格的清理结果
- macOS DMG mount point 由 Go 侧 `os.MkdirTemp` 创建并传给辅助脚本，不在 shell 中生成不受控路径

## 稳定性运行边界

- HTTP 服务使用读取头超时、空闲超时和请求头大小上限；单个请求 panic 返回 500 并记录堆栈，不结束进程
- 单个后台任务 panic 后会结束当前任务并保留重试/排障状态；关闭阶段若等待任务异常或超时，会优先保留 SQLite 进程状态，避免在未知写入状态下继续关闭数据库
- 迁移失败只回滚当前 migration，已完成的前序 migration 不自动回滚；业务服务不会在 schema 未就绪或恢复被阻断时继续启动

## Windows 部署

- `cmd/server/main.go` 在 Windows 默认 `headless=false`（其他平台默认 `true`），保留系统托盘
- DB 文件名分平台：Windows 默认 `app.db`，其他平台 `animate.db`；旧 `animate.db` 已存在则不切换（向后兼容）
- SQLite 在 Windows 自动追加 `?_pragma=journal_mode(WAL)`，避免 modernc/glebarez SQLite 在 portable 部署下 rollback journal 删不掉的启动崩溃
- `scripts/start.bat` 用 PowerShell `WindowStyle=Hidden` 启动 + PID 文件管理；`Program Files` 安装会被检测并警告
- 一站式 `.bat` 工具箱：`init-config` / `open-config` / `open-data` / `open-ui` / `view-logs`，配 `WINDOWS_QUICKSTART.txt` 给非技术用户

## 测试约定

- 每个 store 都有对应 `_test.go`，至少覆盖：nil safety + happy path + 边界
- 用 `db.InitDB(":memory:")` + `t.Cleanup(...)` 跑独立 SQLite，不依赖外部服务
- 外部 HTTP 适配器（bangumi / tmdb / parser 等）用 `httptest.Server` mock
- 不写依赖真实 qBittorrent / Jellyfin / TMDB 的集成测试

### 文档站

文档正文以 `docs/` 下的 Markdown 为唯一来源。修改导航、外部链接或 API 契约后，至少运行：

```bash
python -m pip install -r requirements-docs.txt
mkdocs build --strict
python -m openapi_spec_validator docs/openapi.yaml
```

Pages 工作流会在发布前重复严格构建和 OpenAPI 校验。

## 离线 CLI

- `cmd/doctor` —— 只读，输出 JSON 体检报告（订阅 / 下载 / 本地库 / 配置完整性）
- `cmd/repair` —— 写库，支持 `--dry-run` 列出将执行的动作而不真正写

新增运维 CLI 时**优先做成只读**，写操作必须配 `--dry-run`。

## 不再做的事（避免回退）

- ❌ 在 handler 里直接 `db.DB.Where(...)` —— 走 store
- ❌ 自己 `resty.New()` —— 走 `httpx.NewRestyClient`
- ❌ 在 service / handler 里读 `model.GlobalConfig` —— 走 `configValue(key)`
- ❌ 改老的 migration —— 追加新的
- ❌ shell 拼接路径 / 在 bash 里调用 `mktemp` —— 在 Go 侧创建后作为参数传入
- ❌ 把外部凭据 hard-code 进代码 —— 走配置页
