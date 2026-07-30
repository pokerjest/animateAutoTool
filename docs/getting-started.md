# 快速开始

这条路径适合第一次部署。想了解 Docker、源码构建或公网访问，请从侧边栏进入对应章节。

## 1. 准备 qBittorrent

1. 安装 [qBittorrent][qbittorrent]。
2. 打开 **工具 → 选项 → Web UI**，启用 Web 用户界面。
3. 记下 Web UI 地址、端口、用户名和密码。
4. 先让 qBittorrent 在本机或同一局域网内可访问。

AnimateTool 使用的是 qBittorrent Web UI 账号，不需要额外申请第三方 API Key。可以在 [qBittorrent Web API 文档][qbittorrent-webui] 查看接口行为。

## 2. 下载并启动 AnimateTool

从 [最新发行版][releases] 下载与你的系统和 CPU 架构对应的压缩包。

=== "Windows"

    1. 解压到固定目录。
    2. 复制 `config.yaml.example` 为 `config.yaml`。
    3. 双击 `start.bat`。
    4. 浏览器打开 `http://localhost:8306`。

=== "macOS / Linux"

    ```bash
    cp config.yaml.example config.yaml
    chmod +x start.sh scripts/*.sh
    ./start.sh
    ```

    然后打开 `http://localhost:8306`。

## 3. 完成首次初始化

首次初始化必须在运行程序的同一台电脑上通过 `localhost` 完成：

1. 点击“开始初始化”。
2. 设置管理员用户名和至少 8 个字符的密码。
3. 登录后进入“系统设置”。
4. 填写 qBittorrent 连接信息并点击测试。
5. 配置下载根目录和自动整理模板。

详见[首次初始化与安全边界](first-run-security.md)。

## 4. 添加第一条订阅

1. 打开“订阅管理”。
2. 搜索番剧或粘贴 Mikan RSS。
3. 选择字幕组、清晰度和字幕语言。
4. 先预览最近资源，再保存订阅。
5. 点击“立即检查”验证 qBittorrent 是否收到任务。

!!! tip
    如果资源没有下载，先看订阅详情中的“最近一次运行”和[连接排障](troubleshooting/integrations.md)，不要先重复点击同步。

## 5. 验收清单

- `GET /api/v1/session` 能返回当前登录状态；
- qBittorrent 连接测试成功；
- 订阅预览能看到最近 RSS 条目；
- 下载目录中出现文件，且被整理为 `系列/Season 01/`；
- Jellyfin 扫描后能看到对应番剧；
- 备份页能导出一个 AES-256 加密 ZIP，并能使用对应密码进入恢复分析预览。
