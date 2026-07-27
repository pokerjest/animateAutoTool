# Jellyfin、AList 与播放线路

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

## AList

[打开 AList 官网][alist]{ .md-button .md-button--primary }
[打开 AList 认证 API 文档][alist-auth]{ .md-button }

填写：

```text
alist_url   = https://files.example.com
alist_token = <AList 管理后台生成的 Token>
```

按 [AList 认证 API][alist-auth] 使用管理员或专用账号登录：

```bash
curl -X POST "https://files.example.com/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"<ALIST_USERNAME>","password":"<ALIST_PASSWORD>"}'
```

复制响应中的 `data.token` 填入 `alist_token`。AList 文档说明登录 Token 有有效期；外部 AList 的 Token 过期后，需要重新登录生成并更新。AnimateTool 当前会调用存储列表等管理端接口，因此该 Token 必须具备相应管理员权限；请通过局域网、VPN 或访问控制限制 AList 后台，不要把管理员 Token 暴露给浏览器端。

## 连接验证

1. 先用浏览器或 `curl -I` 验证服务 URL；
2. Jellyfin 在 AnimateTool 设置页点击连接测试；
3. AList 使用下面的请求验证 Token：

```bash
curl "https://files.example.com/api/me" \
  -H "Authorization: <ALIST_TOKEN>"
```

4. 进入播放器或媒体库确认返回的是正确实例，而不是旧地址；
5. 若经过反向代理，检查 `X-Forwarded-Proto` 和 `public_url`。
