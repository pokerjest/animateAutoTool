# 故障排查总览

排障时按层次进行，不要把“域名解析、端口可达、应用登录、外部 API”混成一个问题。

## 四层检查

| 层 | 命令/页面 | 典型结论 |
| --- | --- | --- |
| 本机服务 | `curl -I http://127.0.0.1:8306` | 应用是否监听 |
| DNS | `nslookup` / `Resolve-DnsName` | 域名是否指向正确地址 |
| TCP/UDP | `nc -vz` / `Test-NetConnection` | 入口端口是否可达 |
| 应用与凭据 | 设置页、日志、健康报告 | Token、Cookie、代理和权限 |

## 先收集什么？

- 应用版本和操作系统；
- 脱敏后的完整错误行；
- 发生时间和触发操作；
- 本机服务探测结果；
- 是否经过 Docker、VPN、TUN、双重 NAT 或 CGNAT；
- 不要提交完整配置、密码、Token 或公开 Tunnel URL。

进入：

- [API 与服务连接](integrations.md)
- [日志、健康检查与诊断包](diagnostics.md)
- [Windows Cloudflare Tunnel 排障实录](../remote-access/cloudflare-troubleshooting.md)
