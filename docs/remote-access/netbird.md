# NetBird：私人网络播放

[打开 NetBird 文档][netbird-docs]{ .md-button .md-button--primary }
[打开 NetBird 客户端安装说明][netbird-install]{ .md-button }

NetBird 适合没有公网 IP、只允许自己设备访问，并希望视频流绕开公共 CDN 的场景：

```text
手机 / 平板（NetBird）
        ↓ P2P；无法直连时使用 Relay
Windows 上的 AnimateTool（NetBird）
        ↓
Jellyfin
```

## 安装与连通

1. 在运行 AnimateTool 的 Windows 上安装 NetBird；
2. 在手机或平板安装 NetBird，并登录同一账号；
3. 在 Windows 查看连接状态和 NetBird IP：

   ```powershell
   netbird status -d
   ```

4. 手机连接 NetBird 后访问：

   ```text
   http://<Windows-NetBird-IP>:8306
   ```

AnimateTool 默认监听配置端口的所有本机网络接口，因此通常不需要修改监听地址。若无法连接，请检查 Windows 防火墙是否允许 TCP 8306 从 NetBird 网络进入。

## 配置播放器代理线路

在 **系统设置 → 媒体服务 → Jellyfin** 填写：

```text
AnimateTool NetBird 地址：http://<Windows-NetBird-IP>:8306
```

保存后，在同一张 Jellyfin 设置卡片的 **本设备播放线路** 中选择 **NetBird 代理**。播放器只显示当前实际使用的线路，不再放置切换按钮。该线路具备以下特性：

- 视频请求经过 NetBird，不经过 Cloudflare Tunnel；
- 复用 AnimateTool 已有的 Jellyfin Range 流代理；
- 使用短时签名 URL，不依赖 Cloudflare 域名下的登录 Cookie；
- 不在浏览器 URL 中暴露 Jellyfin API Key；
- NetBird 线路失败或持续卡顿时，自动回退 AnimateTool 代理。

## HTTPS 与混合内容

如果 AnimateTool 控制页面通过 Cloudflare HTTPS 打开，浏览器通常不允许播放器再加载 `http://<NetBird-IP>` 视频。可以选择：

- 为 NetBird 内部地址配置可信 HTTPS，并把 `netbird_proxy_url` 填为 `https://...`；
- 直接从 NetBird HTTP 地址打开 AnimateTool，让页面与视频保持同一安全级别。

不要忽略浏览器的混合内容警告，也不要为了绕过限制关闭整台设备的浏览器安全策略。

## 验收

1. 手机关闭 Wi-Fi，只保留移动网络；
2. 连接 NetBird；
3. 在系统设置中选择 **NetBird 代理**，再打开 AnimateTool 播放器；
4. 拖动进度条，确认 `206 Partial Content` 能正常返回；
5. Windows 执行 `netbird status -d`，观察连接是 P2P 还是 Relayed；
6. 停止手机 NetBird，确认播放器会回退到 AnimateTool 代理。
