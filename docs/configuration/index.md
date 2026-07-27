# 配置总览

AnimateTool 的设置页会把业务配置保存到数据库，并同步镜像到 `config.yaml` 的 `system_settings` 段，方便备份和迁移。

## 配置优先级

1. 运行时启动参数和 `server` / `database` / `auth` 等核心 YAML 配置；
2. Web 设置页保存的 `system_settings`；
3. 外部服务连接测试结果；
4. 任务运行时读取的当前配置。

修改设置后建议重启一次，再用连接测试或健康页确认。

## 字段分类

| 分类 | 代表字段 | 文档 |
| --- | --- | --- |
| 下载器 | `qb_url`、`qb_username`、`qb_password` | [qBittorrent 与自动整理](downloader.md) |
| 媒体服务 | `jellyfin_url`、`jellyfin_api_key`、`alist_token` | [媒体服务](media-services.md) |
| 元数据 | `tmdb_token`、`anilist_token`、`bangumi_access_token` | [元数据 API](metadata-apis.md) |
| AI | `ai_base_url`、`ai_model`、`ai_api_key` | [AI](ai.md) |
| 备份 | `r2_endpoint`、`r2_bucket`、`r2_access_key`、`r2_secret_key` | [R2](r2-backup.md) |
| 网络 | `proxy_url` 和各服务开关 | [网络代理](proxy.md) |

## 配置备份注意事项

系统设置备份可能包含外部服务凭据。分享诊断信息时使用“健康诊断导出”或脱敏后的配置片段，不要直接上传完整数据库和 `config.yaml`。
