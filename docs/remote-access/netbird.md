# NetBird 兼容说明

AnimateTool 仍保留旧 NetBird 配置键、签名接口和流媒体路由，以兼容升级前的客户端或旧链接。当前前端已移除 NetBird 字段、线路按钮和推荐配置；新部署请使用 AnimateTool 代理或 Jellyfin 直连。

旧配置无需删除；如果旧浏览器保存了 `player.sourceMode=netbird`，应用会迁移为 `player.preferredSource=proxy`。旧 `/netbird/*` 接口仍要求有效签名，并继续保留 Range 流兼容行为，但新播放器不会再请求这些地址。

该兼容页面不再提供安装、配置或排障步骤，避免把已屏蔽的线路误认为当前产品能力。
