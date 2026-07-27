# Cloudflare Tunnel

Cloudflare Tunnel 适合没有公网 IPv4、处于 CGNAT 或不想开放入站端口的场景。`cloudflared` 从本机主动建立出站连接，外部请求由 Cloudflare 边缘转发到本地服务。

[打开 Cloudflare Tunnel 文档][cloudflare-tunnel-docs]{ .md-button .md-button--primary }
[打开 Quick Tunnel 文档][cloudflare-quick-tunnels]{ .md-button }
[打开 Cloudflare Access 文档][cloudflare-access]{ .md-button }

## 先验证本机服务

```powershell
curl.exe -I http://127.0.0.1:8306
```

## Quick Tunnel：只用于测试

下载 Windows 64 位 `cloudflared`，放在固定目录：

```powershell
.\cloudflared-windows-amd64.exe tunnel `
  --protocol http2 `
  --edge-ip-version 4 `
  --url http://127.0.0.1:8306
```

日志会生成一个随机 `trycloudflare.com` 地址。窗口必须保持运行，按 `Ctrl+C` 后地址立即失效。

只出现：

```text
Your quick Tunnel has been created
```

还不代表隧道真正上线。必须继续看到：

```text
SUMMARY: Environment is healthy
Registered tunnel connection
```

## Named Tunnel：长期使用

1. 在 Cloudflare Dashboard 创建 Tunnel。
2. 在 Windows 选择 64-bit 安装方式。
3. 用管理员 PowerShell 执行控制台生成的 `service install <TOKEN>` 命令。
4. 添加 Published Application：
   - Hostname：你的 Cloudflare 域名；
   - Service type：`HTTP`；
   - URL：`http://localhost:8306`。
5. 用 [Cloudflare Access][cloudflare-access] 创建只允许自己邮箱的策略。
6. 把应用配置为最终 HTTPS 地址：

```yaml
server:
  public_url: "https://anime.example.com"
  trusted_proxies:
    - 127.0.0.1
```

官方 Windows 服务说明见 [Cloudflare 文档][cloudflare-windows-service]。

## 7844、QUIC 与 HTTP/2

Tunnel 需要出站连接 Cloudflare 的 TCP/UDP 7844。QUIC 使用 UDP，HTTP/2 使用 TCP。

当 QUIC 不稳定时，先强制 HTTP/2：

```powershell
.\cloudflared-windows-amd64.exe tunnel `
  --protocol http2 `
  --edge-ip-version 4 `
  --url http://127.0.0.1:8306
```

## 安全建议

- Quick Tunnel 是公开临时入口，不适合长期部署；
- Named Tunnel Token 不要写进公开文档、截图或 Git；
- 只发布 AnimateTool，不发布路由器后台；
- 对管理入口启用 Access 邮箱策略；
- 大文件播放速度仍受本机上行带宽和 Cloudflare 路径影响。
