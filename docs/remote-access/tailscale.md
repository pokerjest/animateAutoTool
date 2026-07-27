# Tailscale：私有访问与 Funnel

[打开 Tailscale 安装文档][tailscale-install]{ .md-button .md-button--primary }
[打开 Serve 文档][tailscale-serve]{ .md-button }
[打开 Funnel 文档][tailscale-funnel]{ .md-button }

## 普通 Tailnet VPN

适合只让自己的设备访问：

```text
手机/笔记本（安装 Tailscale）
        ↓ Tailnet
Windows / NAS 上的 AnimateTool
```

访问设备通常也需要加入同一个 Tailnet。可以使用 Tailscale IP 或 MagicDNS 地址配置 `jellyfin_direct_url`。

## Funnel

Funnel 把 Tailnet 中的一台设备发布为公开 HTTPS 入口，外部浏览器无需安装 Tailscale。它适合临时分享或小范围测试，但入口公开，性能受当前中继和运营商线路影响。

不要把 Funnel 当作“普通 VPN 的公开版”：

- 普通 VPN：访问端需加入 Tailnet，默认私有；
- Funnel：访问端无需客户端，默认是公开网页入口。

## AnimateTool 配置建议

- Jellyfin 直连可填 Tailnet 地址；
- AnimateTool 主入口仍建议用 Cloudflare Named Tunnel 或受控反向代理；
- 如果启用 IP 白名单，白名单只写自己控制的 Tailnet 网段；
- 不要把 Tailscale Exit Node、Fake-IP TUN 和 Cloudflare Tunnel 初次排障同时开启。

## 性能判断

访问速度取决于两端网络、是否直连、是否经过 DERP、中继位置和服务类型。网页请求与大视频流的体验可能不同，不能仅凭一次页面加载得出绝对结论。
