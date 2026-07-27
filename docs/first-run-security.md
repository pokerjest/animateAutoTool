# 首次初始化与安全边界

## 为什么必须从 localhost 初始化？

初始化阶段还没有正式的管理员密码。应用只允许运行程序的本机通过 `localhost` 创建 bootstrap 会话，远程请求、反向代理请求和伪造转发头都会被拒绝。

初始化完成后：

- 普通访问使用同源 HttpOnly Cookie 会话；
- 所有写操作执行同源校验；
- 登录失败、改密、恢复、备份恢复和敏感设置会写入审计日志；
- 设置页只显示密钥状态，不回显明文；
- `/recover` 仍然只接受本机直连。

## 最小安全配置

```yaml
server:
  port: 8306
  public_url: "https://anime.example.com"
  trusted_proxies:
    - 127.0.0.1
    - ::1
    - 10.0.0.2

auth:
  secret_key: "请替换为稳定的随机值"
```

### `public_url`

公网访问时填写最终的 HTTPS 地址，例如 `https://anime.example.com`。它参与回调地址、同源校验和页面链接生成。

### `trusted_proxies`

这里只填写你明确控制的 Caddy、Nginx、Traefik 或 Tunnel 入口 IP/CIDR。不要填写 `0.0.0.0/0`、`::/0` 或整段不受控内网。

## IP 白名单免密

系统支持 IPv4、IPv6 和 CIDR 白名单，例如：

```text
192.168.1.20
192.168.1.0/24
100.64.0.0/10
```

白名单只适合可信的局域网或 Tailnet；它不会取代 HTTPS、密码和身份访问控制。首次初始化期间不会启用白名单免密。

## 远程暴露前检查

1. 本机 `curl -I http://127.0.0.1:8306` 有响应；
2. `server.public_url` 是最终 HTTPS 地址；
3. 反向代理传递 `Host`、`X-Forwarded-Proto`、`X-Forwarded-Host` 和 `X-Forwarded-For`；
4. `trusted_proxies` 只包含代理入口；
5. 外部访问使用 Cloudflare Access、VPN 或至少强密码；
6. 不把路由器 Web 管理端口与 AnimateTool 管理端口混用。

!!! danger
    不要把 `config.yaml`、R2 Secret、AI Key、Cloudflare Tunnel Token、Dynu 密码或诊断包上传到公开 Issue、聊天记录或截图。
