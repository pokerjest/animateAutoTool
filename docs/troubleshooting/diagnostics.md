# 日志、健康检查与诊断包

## 常用接口

```text
GET /api/v1/health
GET /api/v1/runtime
GET /api/v1/audit-logs?page_size=50
GET /api/v1/diagnostics/health/export
GET /api/v1/diagnostics/logs/export
```

所有接口都需要登录态；恢复接口只能在本机直接访问。

## 日志阅读顺序

1. 找到第一次失败的时间点；
2. 先查看同一时间段的 `health-YYYYMMDD-HH.log`，再用 request ID、task ID、subscription ID 或 migration ID 关联 `server-YYYYMMDD-HH.log`；
3. 记录错误码、目标主机和 HTTP 状态，区分“认证失败”和“根本连不上”；
4. 查看随后是否出现自动重试成功、恢复完成或 readiness 重新通过；
5. 若涉及迁移/恢复，核对 `data/updates/migration-runs/current.json`、快照 ID 和 SHA256；
6. 若出现卡死、请求堆积或后台任务不退出，导出健康诊断包并查看 `goroutines.txt`；
7. 把重复 watchdog 日志压缩成一段，不要只贴最后一屏。

## 导出前脱敏

检查压缩包和日志中是否包含：

- 公网 IP、DDNS 域名、Cloudflare Tunnel URL；
- `Authorization`、Cookie、Token、Secret、密码；
- 本地绝对路径和用户名；
- 反向代理真实 IP。

健康诊断包会自动脱敏常见密码、Token、API Key、Cookie、Authorization 和带密码 URL，但仍可能包含媒体路径、主机名、代理地址、任务标题和错误详情。只向可信维护渠道提交诊断包；公开 Issue 只粘贴错误码和最小复现步骤。
