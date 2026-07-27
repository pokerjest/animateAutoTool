# 反向代理与 HTTPS

适用于你有公网 IPv4、能控制路由器端口转发，并希望用自己的域名访问 AnimateTool 的场景。

## 推荐路径

```text
外部浏览器
  ↓ HTTPS 443
Caddy / Nginx
  ↓ HTTP 127.0.0.1:8306
AnimateTool
```

路由器只转发 80/443 到反向代理，不要把 8306 直接暴露到公网。

## Caddy 示例

[Caddy 官方文档][caddy-docs]：

```caddyfile
anime.example.com {
    reverse_proxy 127.0.0.1:8306
}
```

Caddy 会负责申请和续期证书。第一次部署时确认 DNS 已指向公网 IP，且 TCP 80/443 能从外部到达。

## Nginx 示例

参考 [Nginx 反向代理文档][nginx-reverse-proxy]：

```nginx
server {
    listen 443 ssl http2;
    server_name anime.example.com;

    ssl_certificate     /etc/letsencrypt/live/anime.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/anime.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8306;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 3600;
    }
}
```

## AnimateTool 配置

```yaml
server:
  public_url: "https://anime.example.com"
  trusted_proxies:
    - 127.0.0.1
    - ::1
```

如果代理与 AnimateTool 不在同一台主机，把 `127.0.0.1` 换成反向代理实际源地址或受控 CIDR。

## 验收

```bash
curl -I https://anime.example.com
curl -I https://anime.example.com/api/v1/session
```

手机关闭 Wi-Fi 后再访问。若页面能打开但登录循环、Cookie 不保存或播放器提示混合内容，优先检查 `public_url`、`Host` 和 `X-Forwarded-*`。
