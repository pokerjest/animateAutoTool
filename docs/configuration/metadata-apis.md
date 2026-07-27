# 元数据 API：TMDB、AniList、Bangumi

## 配置总览

| 服务 | 是否必须 | 配置字段 | 官方入口 |
| --- | --- | --- | --- |
| TMDB | 推荐 | `tmdb_token` | [API 设置][tmdb-api] |
| AniList | 可选 | `anilist_token` | [API 文档][anilist-docs] |
| Bangumi | 推荐 | `bangumi_app_id`、`bangumi_app_secret`、`bangumi_access_token` | [应用管理][bangumi-app] |

Mikan RSS 本身不需要 API Key；订阅入口来自 [Mikan Project][mikan]。

## TMDB

[打开 TMDB API 设置][tmdb-api]{ .md-button .md-button--primary }
[打开 TMDB 官方文档][tmdb-docs]{ .md-button }

1. 登录 TMDB，打开 [API 设置][tmdb-api]。
2. 创建 API 应用并完成用途说明。
3. 复制 **API Read Access Token**，不要把个人密码当作 Token。
4. 在设置页填写 `tmdb_token`，保存后运行 TMDB 连接测试。

官方接口说明见 [TMDB 开发者文档][tmdb-docs]。

验证目标：

```bash
curl -H "Authorization: Bearer <TMDB_READ_ACCESS_TOKEN>" \
  "https://api.themoviedb.org/3/configuration"
```

## AniList

[打开 AniList API 文档][anilist-docs]{ .md-button .md-button--primary }
[打开 AniList OAuth 指南][anilist-oauth]{ .md-button }

公开的 GraphQL 查询通常不需要用户 Token。只有需要读取或修改用户账户数据时，才需要 OAuth 应用和授权流程。

- 文档：[AniList API][anilist-docs]
- OAuth 说明：[AniList OAuth][anilist-oauth]
- 应用字段：在 AnimateTool 中填写 `anilist_token`；没有用户授权需求时可留空。

验证公开 GraphQL：

```bash
curl -H "Content-Type: application/json" \
  -d '{"query":"{ Media(search: \"Frieren\", type: ANIME) { id title { romaji } } }"}' \
  https://graphql.anilist.co
```

## Bangumi

[打开 Bangumi 应用管理][bangumi-app]{ .md-button .md-button--primary }
[打开 Bangumi API 文档][bangumi-api]{ .md-button }

1. 打开 [Bangumi 应用管理][bangumi-app]。
2. 创建 OAuth 应用，保存 App ID 与 App Secret。
3. 按 [Bangumi API 项目说明][bangumi-api] 完成授权，取得 Access Token。
4. 在 AnimateTool 中填写：

```text
bangumi_app_id
bangumi_app_secret
bangumi_access_token
```

Access Token 失效时不要把新的 Token 贴到日志；重新授权并在设置页保存即可。

## 代理与限流

如果某个元数据源在当前网络不可达，使用[网络代理](proxy.md)按服务单独开启代理。不要为了“全部能访问”而把所有服务都强制经过同一个不稳定代理。
