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
2. 记录错误码、目标主机和 HTTP 状态；
3. 区分“认证失败”和“根本连不上”；
4. 查看随后是否出现自动重试成功；
5. 把重复 watchdog 日志压缩成一段，不要只贴最后一屏。

## 导出前脱敏

检查压缩包和日志中是否包含：

- 公网 IP、DDNS 域名、Cloudflare Tunnel URL；
- `Authorization`、Cookie、Token、Secret、密码；
- 本地绝对路径和用户名；
- 反向代理真实 IP。

只向可信维护渠道提交诊断包。公开 Issue 只粘贴错误码和最小复现步骤。
