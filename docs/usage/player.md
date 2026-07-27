# 播放器与继续观看

## 三条线路

| 线路 | 适合场景 | 注意事项 |
| --- | --- | --- |
| Jellyfin 直连 | 局域网、Tailnet 或浏览器可直接访问 Jellyfin | 观看设备必须能访问 `jellyfin_direct_url` |
| NetBird 代理 | 手机和 AnimateTool 主机均已连接 NetBird | 填写 `netbird_proxy_url`，视频使用短时签名地址 |
| AnimateTool 代理 | Cloudflare Tunnel、反向代理或外部手机 | 视频流经过 AnimateTool 主机，消耗上行带宽 |

播放器默认使用 AnimateTool 代理，并记住当前浏览器在 **系统设置 → 媒体服务 → Jellyfin** 中选择的线路。播放器本身只展示当前实际线路，不再提供切换按钮。NetBird 代理或 Jellyfin 直连持续卡顿、加载失败时，会从当前位置回退到 AnimateTool 代理，但不会覆盖设置中的首选线路；播放下一集时会重新尝试首选线路。

### NetBird 代理数据路径

```text
Cloudflare / 其他入口上的控制页面
        ↓ 播放器取得短时签名 URL
手机浏览器
        ↓ NetBird 私人网络
AnimateTool NetBird 地址
        ↓ Range 流代理
Jellyfin
```

签名 URL 有效期为 12 小时，并绑定媒体提供商、字符串媒体 ID、当前用户和过期时间。旧的本地剧集数字 ID 仍然兼容。NetBird 媒体接口不接受普通未签名请求，也不会把签名参数继续转发给 Jellyfin。

若控制页面使用 HTTPS，浏览器会阻止 HTTP 视频地址。此时有两种做法：

1. 给 NetBird 内的 AnimateTool 地址配置可信 HTTPS；
2. 直接从 `http://<NetBird-IP>:8306` 打开 AnimateTool，让页面和视频都走 NetBird。

## 继续观看

AnimateTool 保存本地用户播放历史，并在 Jellyfin 可用时同步观看进度。Jellyfin 暂时不可用时，本地继续观看仍然可以显示最近位置。

## 外网播放验收

1. 用手机关闭 Wi-Fi；
2. 访问最终 HTTPS 地址；
3. 播放一个短视频；
4. 打开浏览器网络面板确认视频请求没有被混合内容拦截；
5. 暂停后重新进入，检查位置是否恢复。

长视频更容易暴露家庭宽带上行、代理节点和缓存策略问题。页面打开很快并不代表大文件流一定快。
