# 备份、恢复与诊断

## 三种备份

- **全量备份**：数据库、设置、订阅、元数据和用户数据；
- **系统设置备份**：适合把服务凭据迁移到新环境；
- **Cloudflare-only**：只导出 R2 连接信息，不清空当前其他设置。

恢复前先使用“分析备份”，确认文件类型和可恢复内容，再执行恢复。

## 健康检查

设置页中的健康报告会检查：

- qBittorrent、Jellyfin、AList、R2 是否已配置；
- `server.public_url` 是否为 HTTPS；
- `trusted_proxies` 是否过于宽泛；
- 运行时 goroutine、堆内存、GC 和运行时长；
- 订阅是否长时间未更新；
- 媒体库是否存在未匹配或缺失文件。

## 诊断包

导出的诊断包应优先提交给维护者，而不是直接发到公开 Issue。导出前仍需人工检查压缩包内的服务地址、错误详情和路径。

## 常用 API

```text
GET /api/v1/health
GET /api/v1/runtime
GET /api/v1/diagnostics/health/export
GET /api/v1/diagnostics/logs/export
```

更多接口见 [REST API 参考](../api.md)。
