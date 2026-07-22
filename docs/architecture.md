# 架构指南

本文记录 Animate Auto Tool 的关键架构约定。新增功能或重构时请优先沿用，避免再次走回散落直连数据库的老路。

## 目录全景

```
cmd/
├── server/           # 主服务
├── doctor/           # 离线体检 CLI（只读）
├── repair/           # 离线修复 CLI（写库，支持 --dry-run）
├── debug_metadata, debug_rss, fix_orphans, fix_duplicates, migrate_metadata
internal/
├── api/              # Gin handler + 中间件 + view layer
├── service/          # 业务逻辑 + worker 入口 + access helper
├── store/            # 数据访问层（每个领域一个 *Store）
├── model/            # GORM model 定义
├── db/               # 连接初始化 + 显式 migration
├── httpx/            # 统一的 HTTP client 工厂
├── parser/           # RSS / 文件名 / 标题解析
├── downloader/       # qBittorrent 适配
├── alist, anilist, bangumi, jellyfin, tmdb/   # 外部服务适配
├── launcher/         # 子进程托管（qB / Alist / Jellyfin）
├── updater/          # 应用自更新（GitHub Release）
├── scheduler, worker, event/                  # 定时任务 + 事件总线
├── config, security, safeio, bootstrap, version, tray, renamer/
└── ...
pkg/rss/              # 对外可复用的 RSS 包
web/                  # Vue 前端源码 + Vite 产物 + embed.go
scripts/              # 部署/打包脚本
docs/                 # 本文件、release-checklist、mobile-qa-checklist
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

**所有外部 HTTP 调用**（Bangumi / TMDB / AniList / Jellyfin / AList / qBittorrent / Mikan）统一通过 `httpx.NewRestyClient` 创建：

```go
client := httpx.NewRestyClient(timeout, proxyURL, headers)
resp, err := httpx.NewRequest(ctx, client).Get(url)
```

约定：
- 每个外部 client 都暴露**两套方法**：`Foo()` 和 `FooContext(ctx)`，前者只是 `FooContext(context.Background())` 的便捷包装。新代码请走 ctx 版本。
- 超时由调用方传入，不要在 httpx 里写死默认值。
- proxy / UA / headers 通过参数注入，不读全局配置。
- `httpx.NewHTTPClient` / `NewHTTPClientWithProxy` 同样基于 `newHTTPTransport`，**显式禁用环境变量代理**并强制 30s connect / 30s keep-alive，避免被系统 `HTTP_PROXY` 影响。

## AI 能力：`internal/ai`

`internal/ai` 提供与 OpenAI 兼容（`/v1/chat/completions` + `/v1/models`）的最小客户端 + 工具注册机制，给后续在番剧匹配 / 元数据归类 / 自动诊断等场景调用 LLM 用。

```go
client := ai.NewClient(baseURL, apiKey, model)
resp, err := client.CreateChatCompletion(ctx, ai.ChatCompletionRequest{
    Messages: []ai.ChatMessage{{Role: "user", Content: "..."}},
    Tools:    registry.GetToolDefinitions(),
})
```

约定：
- `NewClient(baseURL, apiKey, model)`：`baseURL` 留空回退到官方 `https://api.openai.com/v1`，自动剥末尾 `/`。
- HTTP 走 `httpx.NewHTTPClient(60s)` —— **复用统一 Transport**，自动禁用环境代理；不要直接 `http.DefaultClient`。
- 请求体走 OpenAI 兼容 schema（`ChatMessage`/`Tool`/`ToolCall`），换其他模型供应商时**只改 baseURL/apiKey/model**，不改类型。
- 工具调用用 `Registry`：`Register(name, description, params, handler)` → 模型回 `tool_calls` 时调 `ExecuteTool(ctx, name, args)` 执行。
- `JSONSchemaObject` / `JSONSchemaProperty` 是参数 schema 的便捷构造，避免每个工具自己拼 map。
- **凭据存哪**：API Key 走 `global_configs`（参考 bangumi/tmdb token 的存储位置），用 `configValue(key)` 读取；不要写进配置文件或环境变量。

何时引入 AI 调用：
- ✅ 番剧标题归一化（中英日多语对齐）、元数据冲突仲裁、订阅疑似缺集的自然语言总结
- ❌ 不要把 AI 用在**正确性敏感**的链路（重命名规则、文件移动、数据库 schema 演进）—— 这些场景模型幻觉成本太高
- 调用都要带超时与降级：LLM 不可用时退到现有的 deterministic 路径

## 数据库迁移：`internal/db/migrations.go`

显式 schema migration，**只追加不修改**：

```go
var migrations = []migration{
    {ID: "001_initial_schema",            Apply: ...},
    {ID: "002_subscription_run_log",      Apply: ...},
    {ID: "003_subscription_strategy_fields", Apply: ...},
}
```

规则：
- **新增字段 / 新表** → `tx.AutoMigrate(&model.X{})` 即可
- **改列名 / 改类型 / 收紧约束 / 数据搬迁** → 必须**新写一条 migration**，不要修改老的
- 启动时 `schema_migrations` 表追踪已应用版本，启动日志会打印当前版本号
- 数据修复脚本也**走 migration**，不要散落进业务启动代码

## HTTP 路由与中间件

入口在 `internal/api/routes_embedded.go`，资源用 `embed.FS` 嵌入二进制。

中间件分层（已在 routes_embedded 装配）：
1. `SecurityHeadersMiddleware` — CSP / XFO / Permissions-Policy 等浏览器侧硬约束
2. `BootstrapLocalOnlyMiddleware` — 首次初始化前**只允许 localhost 直连**
3. `SameOriginMiddleware` — 任何写操作都要求同源（避免 CSRF）
4. `AuthMiddleware` — 会话校验，未登录 401 / 重定向 `/login`
5. `DirectLocalOnlyMiddleware` — 仅用于 `/recover`，强制 loopback + 拒绝 forwarded headers

## 配置与安全

- 业务密码存 bcrypt，bootstrap admin 初始密码写 `data/bootstrap/admin.json`，首次改密后失效
- `auth.secret_key` 留空会自动落到 `data/bootstrap/auth_secret`（不要提交到仓库）
- `server.trusted_proxies` 只填明确控制的反向代理 IP / CIDR
- `server.public_url` 用于生成回调地址 + 同源校验
- 所有外部凭据（qB / Jellyfin / R2 / TMDB / AniList / Bangumi）保存在 `global_configs` 表，通过 `configValue` 读

## 日志

- 主进程 `cmd/server/main.go` 的 `configureLogging` 把 stderr / stdout 同时写到 `logs/server.log`
- 启动时调用 `rotateLogFile`：单文件 ≥ 10 MB 触发滚动，最多保留 5 份（`.1` ~ `.5`）
- 不引入 lumberjack 等第三方依赖

## 自更新（`internal/updater`）

- 拆分为 `manager.go` / `manager_apply.go` / `manager_release_assets.go` / `manager_versions.go`
- macOS DMG 路径：mount point 由 Go 侧 `os.MkdirTemp` 创建并作为 `$6` 参数传入 bash，**不在 shell 里 mktemp**
- Release asset 命名 `<binary>_<version>_<goos>_<goarch>.<ext>`，配合 `SHA256SUMS.txt`
- 校验流程：拉 release → 选 asset → 找 checksum 文件 → 下载 → 比对 SHA256 → 应用

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
