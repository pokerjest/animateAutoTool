# 网络代理

## 代理字段

```text
proxy_url
proxy_bangumi_enabled
proxy_mikan_enabled
proxy_tmdb_enabled
proxy_anilist_enabled
proxy_jellyfin_enabled
proxy_ai_enabled
proxy_updater_enabled
```

支持 HTTP、HTTPS 和 SOCKS5。代理地址应包含协议，例如：

```text
http://127.0.0.1:7890
socks5://127.0.0.1:7891
```

## 推荐配置方式

1. 先只填写 `proxy_url`；
2. 在设置页运行代理测试；
3. 只为实际无法访问的服务打开对应开关；
4. 观察健康页和日志，再扩大范围。

不要让下载、媒体流和 AI 请求无差别经过同一个不稳定节点。Jellyfin 直连与 AnimateTool 代理是独立线路，播放器中可以分别选择。

## 排错

- `invalid_proxy_url`：缺少 `http://`、`https://` 或 `socks5://`；
- `proxy_unreachable`：代理监听端口没有服务；
- `proxy_target_unreachable`：代理可达，但目标网站被节点拦截；
- 只有某一个服务失败：先检查它自己的开关和供应商限流，不要立即修改全局代理。
