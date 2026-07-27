# qBittorrent 与自动整理

[打开 qBittorrent 官网][qbittorrent]{ .md-button .md-button--primary }
[打开 Web API 文档][qbittorrent-webui]{ .md-button }

## qBittorrent Web UI

在 [qBittorrent][qbittorrent] 中打开 **工具 → 选项 → Web UI**：

- 启用 Web UI；
- 设置监听地址和端口；
- 设置用户名与密码；
- 让 AnimateTool 所在主机能够访问该地址。

填写字段：

| 应用字段 | 示例 | 说明 |
| --- | --- | --- |
| `qb_mode` | `external` | 使用外部 qBittorrent |
| `qb_url` | `http://127.0.0.1:8080` | Web UI 根地址 |
| `qb_username` | `anime` | Web UI 用户名 |
| `qb_password` | `••••••••` | Web UI 密码 |
| `base_download_dir` | `/data/downloads` | 下载和整理的媒体根目录 |

qBittorrent 不需要单独申请第三方 API Key；应用使用 Web UI 会话和 Web API。

## 连接验证

在设置页点击连接测试。命令行也可以先检查端口：

```bash
curl -I http://127.0.0.1:8080
```

如果使用 Docker，`127.0.0.1` 指的是 AnimateTool 容器自身，不一定是宿主机。优先使用 Compose 服务名，例如 `http://qbittorrent:8080`。

## 自动整理模板

```yaml
system_settings:
  auto_rename_enabled: "true"
  auto_rename_series_template: "{title}"
  auto_rename_episode_template: "{title} - S{season}E{episode}{ext}"
```

默认结构：

```text
媒体根目录/
└── 系列名/
    └── Season 01/
        └── 系列名 - S01E01.mkv
```

支持 `{title}`、`{season}`、`{episode}`、`{year}`、`{original}` 和 `{ext}`。多文件合集会谨慎跳过自动猜测，避免破坏做种或字幕映射。

## 常见问题

- **登录成功但添加失败**：检查 qBittorrent 返回正文，不要只看 HTTP 200。
- **Cookie 数量为零**：qBittorrent 可能启用了 IP/localhost 免认证，应该以真实 API 请求是否成功为准。
- **文件未整理**：确认下载目录与 `base_download_dir` 在同一容器/主机视图中。
- **做种被破坏**：确认使用的是 qBittorrent 移动和重命名接口，而不是外部文件管理器直接移动。
