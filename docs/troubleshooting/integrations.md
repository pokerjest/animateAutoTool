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

```bash
curl "$AI_BASE_URL/models" \
  -H "Authorization: Bearer $AI_API_KEY"
```

`401` 通常是 Key；`404` 通常是 Base URL；`429` 通常是额度或限流。

## 代理

代理测试成功只代表代理能访问测试目标，不代表每个服务都可用。按服务逐项打开代理开关，并结合日志中的最终 URL 和 HTTP 状态判断。
