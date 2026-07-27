# Animate Auto Tool

<p class="page-lead">
一个面向自托管用户的动漫订阅、下载、整理、元数据聚合与播放工作台。先用最短路径完成部署，再按需接入媒体服务、外部 API、备份和公网访问。
</p>

<div class="quick-facts">
  <span>单机 / NAS / 小型服务器</span>
  <span>Windows · macOS · Linux · Docker</span>
  <span>本机优先，公网访问可选</span>
</div>

<div class="grid cards" markdown>

-   :material-download-circle:{ .lg .middle } __从零开始__

    ---

    用发行包启动服务，完成首次初始化，再接入 qBittorrent。

    [开始快速安装](getting-started.md){ .md-button .md-button--primary }

-   :material-cloud-sync:{ .lg .middle } __自动追番与整理__

    ---

    订阅 Mikan RSS，自动下载、重命名、扫描媒体库并同步 Jellyfin。

    [查看日常使用](usage/subscriptions.md){ .md-button }

-   :material-key-chain:{ .lg .middle } __外部服务 API__

    ---

    每个 Token、API Key 和服务凭据都给出官方申请入口、字段映射和验证方式。

    [配置外部服务](configuration/metadata-apis.md){ .md-button }

-   :material-earth:{ .lg .middle } __从公网安全访问__

    ---

    先判断公网 IPv4、双重 NAT 和 CGNAT，再选择 DDNS、反向代理、Cloudflare Tunnel 或 VPN。

    [打开公网访问决策树](remote-access/index.md){ .md-button }

</div>

## 它解决什么问题？

Animate Auto Tool 是一个适合长期运行在个人电脑、NAS 或小型服务器上的动漫媒体工作台：

- 订阅 [Mikan Project][mikan] RSS 并按字幕组、清晰度、语言和正则筛选；
- 将任务交给 [qBittorrent][qbittorrent]，自动去重、整理和重命名；
- 从 [Bangumi][bangumi-api]、[TMDB][tmdb-docs] 和 [AniList][anilist-docs] 聚合番剧元数据；
- 扫描本地媒体并与 [Jellyfin][jellyfin] 同步播放、收藏和观看进度；
- 将数据库、设置和诊断包备份到本地或 Cloudflare R2；
- 使用本机 Cookie 会话、同源校验、审计日志和受信任代理边界保护管理界面。

## 推荐阅读顺序

1. [快速开始](getting-started.md)
2. [首次初始化与安全边界](first-run-security.md)
3. [qBittorrent 与自动整理](configuration/downloader.md)
4. [媒体服务配置](configuration/media-services.md)
5. [元数据 API 获取与验证](configuration/metadata-apis.md)
6. [公网访问方案决策树](remote-access/index.md)

## 重要链接

- [最新发行版][releases]
- [GitHub 仓库][repo]
- [REST API 与 OpenAPI](api.md)
- [架构说明](architecture.md)
- [贡献指南][contributing]
- [安全漏洞报告][security-policy]

!!! warning "先记住这条安全边界"
    DDNS 只会把域名指向一个已有的 IP，不会创造公网 IP，也不能绕过运营商 CGNAT。对外提供 AnimateTool 时，优先使用 HTTPS 反向代理、Cloudflare Access 或 VPN；不要直接暴露路由器管理后台。
