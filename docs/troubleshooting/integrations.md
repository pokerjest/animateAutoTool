# API 与服务连接排障

## qBittorrent

```bash
curl -I http://127.0.0.1:8080
```

如果端口可达但应用连接失败，检查 Web UI 用户名、密码、URL 是否带错误路径，以及容器之间是否误用了 `localhost`。

## Jellyfin

- `jellyfin_not_configured`：未填写服务 URL；
- `jellyfin_auth_failed`：账号、密码或 API Key 无效；
- `jellyfin_unreachable`：网络、端口、TLS 或代理问题；
- `jellyfin_series_not_found`：服务可达，但媒体库中没有对应条目。

先在 AnimateTool 主机上访问 Jellyfin，再从浏览器测试 `jellyfin_direct_url`。

## R2

检查 Endpoint、Bucket、Access Key 和 Secret Key 是否属于同一 Cloudflare 账户，并确认 Token 拥有目标 Bucket 的读写权限。Secret 泄露后立即删除旧 Token。

## 元数据

- TMDB：确认使用 API Read Access Token，而不是网页密码；
- AniList：公开查询不一定需要 Token；
- Bangumi：检查 App ID、Secret 和 Access Token 是否来自同一个授权流程；
- Mikan：先直接打开 RSS，确认网络和订阅地址本身正常。

## AI

优先在设置页使用当前未保存表单执行“读取模型列表”和“用 hi 测试连接”。这样能按 OpenAI、Gemini 或 Claude 当前选择的原生/兼容格式发送正确请求，避免用一条只适用于 Bearer 鉴权的 `curl` 误判。

`401` 通常是 Key；`404` 通常是 Base URL；`429` 通常是额度或限流。

模型列表可读取不代表模型仍有调用余额；模型目录失败也不一定代表聊天接口不可用。最终以“用 hi 测试连接”为准。AnimateTool 不会在供应商之间自动切换，遇到额度上限时需要手动选择其他模型或供应商。

## 订阅与下载状态

- 下载已经完成但页面仍显示进行中：先执行订阅页“刷新并修复”，同步 qBittorrent 状态并用本地文件回补记录；
- 刷新后出现错误映射：查看资源对账中的冲突证据，不要重复添加任务；同等证据候选不会自动选择；
- 缺集没有补交：确认 RSS 中仍存在对应 canonical 集数，且字幕组、分辨率、语言和包含/排除规则仍允许该条目；
- 下载完成但本地库未出现：检查 AnimateTool 与 qBittorrent 是否看到同一文件路径，以及完成事件后的目标目录增量扫描日志；
- 多文件 torrent 未自动改名：这是保守保护行为，需要在本地媒体页预览后人工整理。

## 代理

代理测试成功只代表代理能访问测试目标，不代表每个服务都可用。按服务逐项打开代理开关，并结合日志中的最终 URL 和 HTTP 状态判断。
