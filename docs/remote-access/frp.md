# FRP：通过 VPS 建立自控反向代理

FRP 适合你拥有一台有公网入口的 VPS，并希望自己控制中转链路的场景。

[打开 FRP 官方文档][frp-docs]{ .md-button .md-button--primary }
[打开 FRP 网络配置][frp-config]{ .md-button }

## 网络结构

```text
外部浏览器
  ↓ VPS:443
frps（VPS）
  ⇄ 加密连接
frpc（家庭 Windows/NAS）
  ↓
127.0.0.1:8306
```

FRP 不会凭空提供公网入口；VPS 的安全组、防火墙、域名和 TLS 都需要自己维护。

## 服务端最小示例

```toml
# frps.toml
bindPort = 7000

auth.method = "token"
auth.token = "replace-with-a-long-random-token"

vhostHTTPPort = 80
vhostHTTPSPort = 443
```

## 客户端最小示例

```toml
# frpc.toml
serverAddr = "vps.example.com"
serverPort = 7000

auth.method = "token"
auth.token = "replace-with-the-same-token"

[[proxies]]
name = "animate-tool"
type = "http"
localIP = "127.0.0.1"
localPort = 8306
customDomains = ["anime.example.com"]
```

生产环境建议：

- VPS 上使用 Caddy/Nginx 终止 TLS；
- FRP 管理端口只允许必要来源；
- Token 使用随机长字符串并定期轮换；
- `server.public_url` 填最终 HTTPS 域名；
- 监控 VPS 磁盘、带宽和 FRP 连接状态。

如果只是为了绕过 CGNAT 且不想维护 VPS，优先选择 [Cloudflare Tunnel](cloudflare-tunnel.md)。
