# 公网访问方案决策树

先把问题拆成三层：

```text
本机服务正常？
  ↓
路由器是否拿到公网 IPv4 / 可用 IPv6？
  ↓
外部设备是否能到达入口端口？
  ↓
再选择 DDNS、反向代理、Tunnel 或 VPN
```

## 第一步：确认本机服务

```bash
curl -I http://127.0.0.1:8306
```

返回 `200`、`301`、`302`、`401` 等 HTTP 响应，都说明本地服务至少有监听。

## 第二步：确认地址类型

在路由器“互联网状态”查看 WAN 地址：

| WAN 地址 | 结论 |
| --- | --- |
| 公网 IPv4 | 可以继续做端口转发和反向代理 |
| `192.168.x.x`、`10.x.x.x`、`172.16.x.x`–`172.31.x.x` | 上级还有一层 NAT |
| `100.64.0.0/10` | 高概率处于运营商 CGNAT |
| 有 `240e:` 等 IPv6，但无默认路由 | IPv6 地址存在，但不能视为可用 |

DDNS 只能让域名跟随地址变化，不能把私网地址变成公网地址。

## 选择方案

| 场景 | 推荐方案 | 需要什么 |
| --- | --- | --- |
| 公网 IPv4，能控制路由器 | Caddy/Nginx + 端口转发 + DDNS | 80/443 入站、域名 |
| 双重 NAT | 光猫桥接；或 DMZ/逐层转发 | 能控制上级设备 |
| CGNAT / 没有公网 IPv4 | Cloudflare Tunnel | Cloudflare 账号；Named Tunnel 需要域名 |
| 只允许自己访问 | Tailscale VPN | 访问设备加入 Tailnet |
| 公开网页但不想开入站端口 | Tailscale Funnel | Tailnet 设备运行 Funnel |
| 有 VPS，想完全自控链路 | FRP | 一台有公网入口的 VPS |
| IPv6 真正可达 | AAAA + IPv6 防火墙 + HTTPS | 默认路由、PD、外部连通 |

## 安全边界

- 对外只暴露 AnimateTool 的 HTTPS 入口，不暴露路由器后台；
- 公网访问优先加 Cloudflare Access、VPN 或额外身份认证；
- `public_url` 必须填写最终 HTTPS 地址；
- `trusted_proxies` 只包含明确控制的反向代理；
- 先从手机 4G/5G 验收，不要只在家中 Wi-Fi 里访问自己的公网域名。

下一步：

- [DDNS 与动态公网地址](ddns.md)
- [反向代理与 HTTPS](reverse-proxy.md)
- [Cloudflare Tunnel](cloudflare-tunnel.md)
