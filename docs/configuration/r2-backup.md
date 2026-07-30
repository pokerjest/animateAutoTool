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

## 验证与上传

```text
GET /api/v1/backup/r2/list
POST /api/v1/backup/r2/test
POST /api/v1/backup/r2/upload
```

当前备份页创建的是完整备份：数据库先被压缩为 ZIP，再使用 AES-256 加密，随后才下载到浏览器或上传到 R2。这样可以减少传输体积，也避免 R2 中保存明文数据库。

创建备份时可以：

- 输入当前管理员登录密码，并把它作为归档密码；
- 或设置至少 8 位的独立备份密码。

密码不会保存到服务器。恢复本地或 R2 备份时必须输入创建该文件时使用的密码；忘记后无法解包。上传和恢复进度会显示在备份页面及任务中心。

底层仍保留旧版“系统设置”和“Cloudflare-only”选择性备份格式的读取兼容。恢复选择性备份时会保留当前设备已有的密码、Token 和 API Key，不会用空值清除凭据。

!!! danger
    完整备份可能包含已经保存的服务凭据。AES-256 加密不能弥补弱密码或密码泄露；不要把归档密码和备份文件放在同一公开位置。R2 Secret Access Key 泄露后应立即删除旧 Token，并创建新的最小权限 Token。
