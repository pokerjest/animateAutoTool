# IPv6：有地址不代表可访问

IPv6 可以绕过没有公网 IPv4 的限制，但必须同时满足：

1. 网卡有全球单播地址；
2. 路由器下发 RA 或 DHCPv6-PD；
3. 主机有默认 IPv6 路由；
4. 路由器和主机防火墙允许入站；
5. DNS 有正确 AAAA；
6. 外部网络可以访问目标端口。

## 检查命令

```bash
ip -6 addr
ip -6 route
curl -6 -I https://anime.example.com
```

macOS 如果看到：

```text
route: writing to routing socket: not in table
curl: (7) Couldn't connect to server
```

通常说明地址存在，但默认路由或入站策略不完整。

## 配置建议

- 为最终域名添加 AAAA；
- 在路由器 IPv6 防火墙中只放行反向代理端口；
- 不要因为 IPv6 地址看起来像公网地址就直接开放 8306；
- 同时保留 IPv4、Cloudflare Tunnel 或 VPN 作为回退路径；
- 使用手机网络和外部 IPv6 测试站点验收。
