# qBittorrent 与自动整理

[打开 qBittorrent 官网][qbittorrent]{ .md-button .md-button--primary }
[打开 Web API 文档][qbittorrent-webui]{ .md-button }

## qBittorrent Web UI

在 [qBittorrent][qbittorrent] 中打开 **工具 → 选项 → Web UI**：

- 启用 Web UI；
- 设置监听地址和端口；
- 设置用户名与密码；
- 让 AnimateTool 所在主机能够访问该地址。

AnimateTool 支持两种运行模式：

- `external`：连接已经运行的 qBittorrent Web UI，适合 NAS、Docker 和已有下载环境；
- `managed`：由 AnimateTool 托管可用的 qBittorrent 子进程，要求发行包或运行环境中存在对应二进制。

填写字段：

| 应用字段 | 示例 | 说明 |
| --- | --- | --- |
| `qb_mode` | `external` | `external` 或 `managed` |
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
  incremental_scan_enabled: "true"
  media_naming_preset: "jellyfin-emby"
  auto_rename_series_template: "{title}"
  auto_rename_episode_template: "{title} - S{season}E{episode}{ext}"
  write_nfo_enabled: "false"
  write_images_enabled: "false"
```

默认结构：

```text
媒体根目录/
└── 系列名/
    └── Season 01/
        └── 系列名 - S01E01.mkv
```

模板支持：

| 变量 | 含义 |
| --- | --- |
| `{title}`、`{original}`、`{year}` | 系列标题、原始标题和年份 |
| `{season}`、`{episode}`、`{episode_end}` | 季度、起始集和多集范围终点 |
| `{episode_type}`、`{absolute_episode}` | SP/OVA 等类型和绝对集数 |
| `{group}`、`{resolution}`、`{version}`、`{language}` | 发布组、分辨率、V2/V3 和语言 |
| `{ext}` | 包含点号的扩展名 |

`jellyfin-emby` 预设会恢复推荐的系列和剧集模板；选择 `custom` 后可以自行调整。

## 下载完成后的处理

自动整理发生在下载完成后，不是在添加任务前把最终文件名交给 qBittorrent：

1. 同步 qBittorrent 的完成状态；
2. 识别单视频任务对应的系列和剧集；
3. 通过 qBittorrent 的重命名和移动接口调整路径，尽量保持做种；
4. 等待文件落盘和移动稳定；
5. 优先对受影响的番剧目录执行增量扫描；
6. 无法安全定位目标时才回退到媒体根目录扫描。

多文件 torrent、候选映射冲突或无法确定集数时会保守跳过自动整理，不会猜一个路径强行移动。V2/V3 也不会被后台任务隐式替换，需要用户明确选择升级版本。

元数据设置中的 `metadata_source_order` 决定 Bangumi、TMDB、AniList 字段冲突时的优先级；`metadata_overwrite_policy` 控制本地 NFO 与网络字段的合并方式。`write_nfo_enabled` 和 `write_images_enabled` 则控制是否生成 sidecar 文件。

## 常见问题

- **登录成功但添加失败**：检查 qBittorrent 返回正文，不要只看 HTTP 200。
- **Cookie 数量为零**：qBittorrent 可能启用了 IP/localhost 免认证，应该以真实 API 请求是否成功为准。
- **文件未整理**：确认下载目录与 `base_download_dir` 在同一容器/主机视图中。
- **做种被破坏**：确认使用的是 qBittorrent 移动和重命名接口，而不是外部文件管理器直接移动。
- **下载完成但仍显示下载中**：先在订阅页执行“刷新并修复”，让应用重新同步 qBittorrent 状态并对账本地文件。
- **两条任务同时完成但只扫描到一条**：检查日志中的完成事件合并和目标目录；应用会分别保留受影响目录并重试尚未稳定的文件。
