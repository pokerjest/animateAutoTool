# Cloudflare R2 备份

[打开 Cloudflare Dashboard][cloudflare-dashboard]{ .md-button .md-button--primary }
[打开 R2 Token 官方文档][r2-api-tokens]{ .md-button }
[打开 R2 官方文档][r2-docs]{ .md-button }

## 需要哪些字段？

| 字段 | 含义 |
| --- | --- |
| `r2_endpoint` | S3-compatible Endpoint，例如 `https://<ACCOUNT_ID>.r2.cloudflarestorage.com` |
| `r2_bucket` | 已创建的 Bucket 名称 |
| `r2_access_key` | R2 S3 Access Key ID |
| `r2_secret_key` | R2 S3 Secret Access Key |

## 创建步骤

1. 在 Cloudflare Dashboard 打开 R2，创建 Bucket。
2. 在 R2 API Tokens 页面创建专用 Token。
3. 只授予目标 Bucket 的 Object Read/Write 权限。
4. 复制 Endpoint、Access Key ID 和 Secret Access Key；Secret 只显示一次。
5. 在 AnimateTool 设置页保存并点击 R2 连接测试。

## 验证与备份模式

```text
GET /api/v1/backup/r2/list
POST /api/v1/backup/r2/test
POST /api/v1/backup/r2/upload
```

应用支持：

- **全量备份**：迁移数据库、设置和媒体索引；
- **系统设置备份**：只迁移连接配置；
- **Cloudflare-only**：只导出 R2 连接信息，恢复时合并到当前设置。

!!! danger
    R2 Secret Access Key 等同于密码。不要把完整配置、导出文件或诊断包上传到公开位置。若泄露，立即在 Cloudflare 删除旧 Token 并创建新的最小权限 Token。
