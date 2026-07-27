# AI 与 OpenAI-compatible 服务

AnimateTool 使用兼容 OpenAI Chat Completions 风格的 HTTP 接口，因此可以连接官方服务，也可以连接其他实现相同协议的供应商或本地网关。

## 获取凭据

[打开 OpenAI API Keys][openai-keys]{ .md-button .md-button--primary }
[打开 OpenAI 官方文档][openai-docs]{ .md-button }

- 官方 OpenAI：打开 [API Keys][openai-keys]，文档见 [平台概览][openai-docs]；
- 其他供应商：使用其官方控制台生成 Key，并确认它提供 OpenAI-compatible Base URL；
- 本地模型网关：确认服务在 AnimateTool 主机可访问，并允许对应模型名称。

## 字段填写

| 字段 | 示例 | 说明 |
| --- | --- | --- |
| `ai_base_url` | `https://api.example.com/v1` | OpenAI-compatible API 根地址 |
| `ai_model` | `<MODEL_ID>` | 供应商控制台中实际可用的模型名 |
| `ai_api_key` | `<API_KEY>` | API Key |
| `proxy_ai_enabled` | `true` | 是否经过全局代理 |

不要把 Base URL 写成聊天网页地址；它必须是 API 端点前缀。

使用 OpenAI 官方服务时，`ai_base_url` 填 `https://api.openai.com/v1`。官方接口使用 Bearer API Key；优先为 AnimateTool 创建独立的 Project Key，不要使用组织管理员 Key，也不要把 Key 写入前端代码、截图或公开仓库。

## 最小验证

在设置页保存后点击“获取模型列表”或运行 AI 连接测试。若需命令行核对：

```bash
curl "$AI_BASE_URL/models" \
  -H "Authorization: Bearer $AI_API_KEY"
```

不同供应商的模型列表权限可能不同；模型列表失败不一定代表聊天接口不可用，最终应以应用的测试结果为准。

## 常见错误

- `401`：Key 无效、已撤销或 Authorization 头格式不正确；
- `404`：Base URL 多了一层 `/chat/completions` 或缺少 `/v1`；
- `429`：余额、速率限制或供应商配额不足；
- `ai_not_configured`：应用没有保存非空的 `ai_api_key`；
- 返回空内容：模型名称与服务端实际部署名称不一致。

!!! warning
    AI 请求可能包含你主动输入的内容。不要把密码、Token、完整配置文件或私人日志粘贴到对话中。
