# API 参考

Animate Auto Tool 的浏览器客户端只使用 `/api/v1` JSON API。除图片、视频流和备份下载外，成功响应统一为 `{ "data": ..., "meta"?: ..., "message"?: string }`，失败响应统一为 `{ "error": { "code": string, "message": string } }`。

OpenAPI 3.1 契约见 [openapi.yaml](openapi.yaml)，TypeScript 类型由该文件生成。旧 `/api/*` 页面片段接口不再在生产路由中注册。

## 安全边界

- 会话使用同源 HttpOnly Cookie；所有写操作执行同源校验。
- `/api/v1/recovery/reset` 只允许 localhost 直接访问，并拒绝转发头。
- 初始化未完成时，所有功能只允许本机访问。
- 设置读取不会返回密钥明文；更新时空白密钥表示保留原值。成功更新会同步写入本机 `config.yaml` 的 `system_settings` 镜像。
- 登录、退出、改密、本机恢复、删除目录/订阅、恢复备份和设置变更写入审计日志。

## 路由概览

| 领域 | 路由 |
| --- | --- |
| 会话 | `GET /session`、`POST /session/login`、`POST /session/logout`、`POST /session/change-password` |
| 初始化与恢复 | `GET /setup/readiness`、`POST /setup/bootstrap`、`POST /recovery/reset` |
| 概览与任务 | `GET /dashboard`、`POST /tasks/sync`、`GET /events` |
| 订阅 | `GET/POST /subscriptions`、`PUT/DELETE /subscriptions/{id}`、检查、启停、历史、批量导入、RSS 校验、Mikan 搜索与修复动作 |
| 日历与图鉴 | `GET /calendar`、`GET /library`、元数据刷新、搜索和手动匹配 |
| 本地媒体 | `GET /local-anime`、目录增删、扫描、重命名预览/执行、元数据源切换、剧集与播放接口 |
| 备份 | `GET /backup`、导出、分析、选择性恢复、R2 上传/暂存/进度/删除/测试 |
| 系统 | `GET /health`、`GET /runtime`、`GET /audit-logs` |
| 设置 | `GET/PUT /settings`、连接测试、部署检查和自更新任务 |
| AI | `POST/DELETE /assistant/messages`、AI 状态与模型列表 |

分页列表接受 `page` 和 `page_size`，并在 `meta` 中返回 `page`、`page_size`、`total`。后台任务返回 `202`，`data` 至少包含 `task_id` 和 `status: "running"`；扫描、下载、元数据和订阅进度通过 `/events` 的类型化 SSE 事件更新。

图片、视频与文件响应是协议例外：`/posters/{id}`、`/jellyfin/stream/{id}` 和 `/backup/export` 返回原始媒体或附件，不套 JSON envelope。
