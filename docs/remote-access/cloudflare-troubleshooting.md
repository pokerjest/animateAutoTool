# Windows + Cloudflare Tunnel 排障实录

本页把一次真实排障过程改写为可复现 runbook。所有域名、IP、Token 和 Connector ID 都是占位符。

## 症状一：节点变成 `198.18.x.x`

如果日志把 Cloudflare 节点解析为 `198.18.0.0/15`，通常是 Clash/Mihomo/TUN/Fake-IP 或其他透明代理接管了 DNS。它不是正常的 Cloudflare 公网边缘地址。

处理顺序：

1. 暂停 `cloudflared`；
2. 临时退出 Clash、Mihomo、WARP、Tailscale Exit Node 和其他 VPN；
3. `ipconfig /flushdns`；
4. 将 `*.argotunnel.com` 加入真实 IP/直连规则；
5. 重新查询 SRV 和 A 记录。

## 症状二：SRV 查询超时

```powershell
Resolve-DnsName _v2-origintunneld._tcp.argotunnel.com -Type SRV
```

先指定公共 DNS 判断是默认 DNS 还是网络本身的问题：

```powershell
Resolve-DnsName _v2-origintunneld._tcp.argotunnel.com `
  -Type SRV -Server 1.1.1.1 -DnsOnly

Resolve-DnsName _v2-origintunneld._tcp.argotunnel.com `
  -Type SRV -Server 223.5.5.5 -DnsOnly
```

查看真实联网网卡：

```powershell
Get-NetAdapter | Where-Object Status -eq "Up"
Get-DnsClientServerAddress -AddressFamily IPv4 |
  Where-Object { $_.ServerAddresses.Count -gt 0 }
```

若 WLAN 使用了路由器 DNS，例如 `192.168.1.1`，用管理员 PowerShell 临时改为：

```powershell
Set-DnsClientServerAddress `
  -InterfaceAlias "WLAN" `
  -ServerAddresses ("1.1.1.1","223.5.5.5")

Clear-DnsClientCache
ipconfig /flushdns
```

网卡名称不是 `WLAN` 时，替换成 `Get-NetAdapter` 显示的物理网卡名称。非管理员 PowerShell 会报无法访问 CIM 资源。

## 症状三：DNS 正常但 7844 失败

```powershell
Test-NetConnection region1.v2.argotunnel.com -Port 7844
Test-NetConnection region2.v2.argotunnel.com -Port 7844
```

如果 TCP 可用但 QUIC 不稳定，强制 HTTP/2；如果两者都失败，检查本机防火墙、代理软件和网络出口。

## 成功判断

完整成功链路应包含：

```text
DNS Resolution ... PASS
TCP Connectivity ... PASS
UDP Connectivity ... PASS
Cloudflare API ... PASS
SUMMARY: Environment is healthy
Registered tunnel connection
```

只得到随机 `trycloudflare.com` URL，而没有 `Registered tunnel connection`，说明连接器尚未上线。

## 外网验收

手机关闭 Wi-Fi 后：

1. 打开 Named Tunnel 域名；
2. 登录 AnimateTool；
3. 播放短视频；
4. 检查 API 请求和静态资源是否正常；
5. 回到家中查看 `cloudflared` 仍在运行。

## 本次排障的结论

- DDNS 成功与否和 Tunnel 是否上线是两个独立问题；
- 有 IPv6 地址不等于 IPv6 有默认路由；
- `Cannot assign requested address`、DNS 超时和 7844 失败要分别定位；
- Cloudflare Tunnel 成功后仍要配置 Access 和应用自己的登录安全。
