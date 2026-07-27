# DDNS 与动态公网地址

## DDNS 能做什么？

DDNS 把：

```text
不断变化的公网 IP → 固定域名
```

它不能解决：

- 路由器 WAN 是私网地址；
- 光猫双重 NAT；
- 运营商 CGNAT；
- 入站端口被防火墙或运营商阻断。

## 免费服务比较

免费层和验证规则会变化，注册前应以官方页面为准：

| 服务 | 适合场景 | 注意事项 |
| --- | --- | --- |
| [Dynu][dynu] | 免费主机名和路由器 DDNS | 可单独设置 IP Update Password |
| [DuckDNS][duckdns] | 简单脚本、少量主机 | 依赖 Token 和定时更新 |
| [No-IP][noip-free] | 常见路由器内置支持 | 免费主机可能需要定期确认 |
| [FreeDNS][freedns] | 免费子域名和动态 DNS | 界面和配置相对传统 |
| [华硕 DDNS][asus-ddns] | 华硕路由器用户 | 绑定路由器生态，能力取决于型号 |
| Cloudflare DNS | 你已有自己的域名 | 不提供免费随机主机名，需 API Token 更新 |

## Dynu 操作流程

[打开 Dynu 动态 DNS][dynu-ddns]{ .md-button .md-button--primary }
[打开 Dynu API 文档][dynu-api]{ .md-button }

1. 在 [Dynu 动态 DNS 页面][dynu-ddns] 注册账号。
2. 创建免费主机名，例如 `home.example-ddns.net`。
3. 在账户安全或 IP 更新设置中创建独立的 IP Update Password。
4. 在路由器 DDNS 页面填写服务商、主机名、用户名和更新密码。
5. 点击应用，等待路由器产生更新日志。

Dynu 的接口文档见 [官方 API 页面][dynu-api]。

## 用命令验证

```powershell
curl.exe -u "Dynu用户名" `
  "https://api.dynu.com/nic/update?hostname=home.example-ddns.net&myip=203.0.113.10&myipv6=no"
```

返回值：

| 返回 | 含义 |
| --- | --- |
| `good` | 记录已更新 |
| `nochg` | 认证成功，当前 IP 已经相同 |
| `badauth` | 用户名、密码或主机名错误 |
| 连接失败 | 当前网络或客户端无法访问 Dynu |

查询 DNS：

```powershell
nslookup home.example-ddns.net 1.1.1.1
```

查询结果应与路由器的公网 WAN IP 一致。建议等待 TTL 后，再从手机网络测试端口。

## 双重 NAT 与 CGNAT

如果外部检测到的公网 IP 与路由器 WAN IP 不同：

```text
互联网
  ↓
光猫/上级路由器
  ↓
主路由器
  ↓
AnimateTool
```

优先把光猫改为桥接；无法桥接时，将 TCP 443（或反向代理端口）逐层转发，或把主路由器 WAN 放入上级设备 DMZ。

如果主路由器 WAN 是 `100.64.0.0/10` 或其他运营商私网地址，通常是 CGNAT。此时不要继续折腾普通端口转发，直接看 [Cloudflare Tunnel](cloudflare-tunnel.md)、[Tailscale](tailscale.md) 或 [FRP](frp.md)。
