# REST API 参考

Animate Auto Tool 的浏览器客户端使用 `/api/v1` JSON API。完整契约见 [`openapi.yaml`](openapi.yaml)，本页解释认证、安全边界和常用调用方式。

## 交互式 OpenAPI

<swagger-ui src="openapi.yaml"/>

如果交互式页面无法加载，可以直接下载 [OpenAPI YAML](openapi.yaml) 导入 Postman、Insomnia 或其他 OpenAPI 工具。

## 基础约定

- Base URL：`https://anime.example.com/api/v1`
- JSON 成功响应：`{ "data": ..., "meta"?: ..., "message"?: "..." }`
- JSON 失败响应：`{ "error": { "code": "...", "message": "..." } }`
- 分页参数：`page`、`page_size`，最大页大小由服务端限制；
- 后台任务通常返回 `202`，`data` 至少包含 `task_id` 和 `status: "running"`；
- `/events` 是类型化 Server-Sent Events；
- 图片、视频流和备份导出返回原始媒体或附件，不套 JSON envelope。

## 认证：浏览器会话 Cookie

项目没有面向公网的静态 API Token。API 使用同源 HttpOnly Cookie 会话，写操作还需要同源校验。

### 登录并保存 Cookie

```bash
curl -c cookies.txt \
  -H "Content-Type: application/json" \
  -d '{"username":"<USERNAME>","password":"<PASSWORD>","remember_me":false}' \
  https://anime.example.com/api/v1/session/login
```

### 查询当前会话

```bash
curl -b cookies.txt \
  https://anime.example.com/api/v1/session
```

### 退出

```bash
curl -b cookies.txt -X POST \
  -H "Origin: https://anime.example.com" \
  https://anime.example.com/api/v1/session/logout
```

!!! warning
    不要把 `cookies.txt`、浏览器 Cookie 或登录请求中的密码提交到 Issue、日志和截图。公网 API 应放在 HTTPS、Cloudflare Access、VPN 或其他受控入口之后。

## 本机初始化与恢复

以下接口只允许本机直连：

```text
POST /api/v1/session/bootstrap
POST /api/v1/recovery/reset
```

远程请求、反向代理转发和伪造 `X-Forwarded-For` 都不能绕过本机限制。

## 常用接口示例

### 健康与运行时

```bash
curl -b cookies.txt https://anime.example.com/api/v1/health
curl -b cookies.txt https://anime.example.com/api/v1/runtime
```

### 订阅列表与立即同步

```bash
curl -b cookies.txt \
  "https://anime.example.com/api/v1/subscriptions?page=1&page_size=20"

curl -b cookies.txt -X POST \
  -H "Origin: https://anime.example.com" \
  https://anime.example.com/api/v1/tasks/sync
```

### 创建订阅

```bash
curl -b cookies.txt -X POST \
  -H "Origin: https://anime.example.com" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "示例番剧",
    "rss_url": "https://mikan.example.invalid/rss.xml",
    "resolution_filter": "1080p",
    "subtitle_language": "chs"
  }' \
  https://anime.example.com/api/v1/subscriptions
```

示例域名不会产生真实订阅；实际使用时替换为 Mikan RSS。

### 审计日志

```bash
curl -b cookies.txt \
  "https://anime.example.com/api/v1/audit-logs?action=login.failure&page_size=50"
```

审计日志只用于事后追溯，不是实时告警系统。

## 路由分组

| 领域 | 代表路由 |
| --- | --- |
| 会话 | `/session`、`/session/login`、`/session/logout`、`/session/change-password` |
| 初始化与恢复 | `/setup/readiness`、`/setup/bootstrap`、`/recovery/reset` |
| 订阅与任务 | `/subscriptions`、`/tasks`、`/events` |
| 元数据与媒体库 | `/calendar`、`/library`、`/metadata/search`、`/local-anime` |
| 播放 | `/jellyfin/stream/{id}`、`/jellyfin/play/{id}`、`/playback/continue`、`/playback/progress` |
| 备份 | `/backup`、`/backup/export`、`/backup/analyze`、`/backup/restore`、`/backup/r2/*` |
| 系统 | `/health`、`/runtime`、`/audit-logs`、`/diagnostics/*` |
| 设置 | `/settings`、`/settings/proxy/test`、`/settings/connections/{provider}` |
| AI | `/settings/ai`、`/settings/ai/models`、`/assistant/messages` |

## 代理与部署边界

反向代理必须传递：

```text
Host
X-Forwarded-Proto
X-Forwarded-Host
X-Forwarded-For
```

并且应用配置：

```yaml
server:
  public_url: "https://anime.example.com"
  trusted_proxies:
    - 127.0.0.1
```

只有来自 `trusted_proxies` 的请求才会采信转发头。不要把 `0.0.0.0/0` 或整个内网加入受信任列表。
