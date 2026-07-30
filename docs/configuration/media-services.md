# Jellyfin 与播放线路

当前媒体模式只实现 Jellyfin。设置页为未来的 Plex、Emby 等提供商保留扩展位置，但这些卡片还不是可用适配器；旧版本遗留的 AList/PikPak 兼容字段也不属于当前前端支持范围。

## Jellyfin

[打开 Jellyfin 官网][jellyfin]{ .md-button .md-button--primary }
[打开 Jellyfin 官方文档][jellyfin-docs]{ .md-button }

填写字段：

| 应用字段 | 用途 |
| --- | --- |
| `jellyfin_url` | AnimateTool 后端访问 Jellyfin 的地址 |
| `jellyfin_direct_url` | 浏览器或手机可以直接访问的地址，可选 |
| `jellyfin_library_ids` | 媒体模式中展示的 Jellyfin 媒体库 ID；空数组表示全部 |
| `jellyfin_username` | Jellyfin 用户名 |
| `jellyfin_password` | Jellyfin 密码，用于连接测试和状态同步 |
| `jellyfin_api_key` | 推荐的长期服务凭据 |

在 Jellyfin 管理后台的 **Dashboard → API Keys** 创建一个只供 AnimateTool 使用的 Key。Jellyfin API Key 不应被视为细粒度只读凭据；请把它当作高敏感服务密钥保存，泄露后立即在 Jellyfin 后台撤销。

### 两条播放线路

- **Jellyfin 直连**：观看设备直接访问 `jellyfin_direct_url`，适合局域网或 Tailnet。
- **AnimateTool 代理**：观看设备只访问 AnimateTool，视频流经过应用主机，适合 Cloudflare Tunnel 或反向代理。

浏览器页面使用 HTTPS 时，Jellyfin 直连地址也应使用 HTTPS，否则会触发混合内容限制。

播放线路在完整播放器的视频下方选择。选择会立即作用于当前浏览器，不会自动回退到另一条线路；旧版本的 `player.sourceMode=netbird` 会自动迁移为 AnimateTool 代理。

媒体库范围也在 Jellyfin 设置卡片中选择。默认展示全部影视媒体库；保存为 `[]` 时同样代表全部，避免升级后因为没有显式选择而显示空库。

## 连接验证

1. 先用浏览器或 `curl -I` 验证服务 URL；
2. 在 AnimateTool 设置页根据当前浏览器保存的播放线路测试 Jellyfin；
3. 代理测试使用后端 `jellyfin_url`，直连测试使用浏览器可访问的 `jellyfin_direct_url`；
4. 进入播放器或媒体库确认返回的是正确实例，而不是旧地址；
5. 若经过反向代理，检查 `X-Forwarded-Proto` 和 `public_url`。

只有 `jellyfin_url` 和 `jellyfin_api_key` 都已保存时，Jellyfin 媒体模式入口才会启用。服务暂时不可达不会禁用入口，媒体页会显示连接错误和设置入口。本地番剧播放器不依赖 Jellyfin 配置。
