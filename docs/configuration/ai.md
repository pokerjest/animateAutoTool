# AI：OpenAI、Gemini 与 Claude

AnimateTool 可以分别保存三套 AI 服务配置，并从中选择一套作为当前生效服务：

- OpenAI / GPT；
- Google Gemini；
- Anthropic Claude。

切换服务不会删除其他供应商的 Key。AI 助手只调用当前选中的一家；请求失败时不会自动切换供应商。

## 凭据类型

三家的模型调用都使用单个 API Key：

- OpenAI API Key；
- Gemini API Key；
- Anthropic API Key。

`google_client_id` / `google_client_secret` 属于 Google OAuth 2.0 客户端凭据，通常用于 Google 登录或代表用户访问 Google 服务，不是本页 Gemini 模型请求所需的 Key。OpenAI 和 Anthropic 的常规模型 API 调用同样不要求 OAuth Client ID / Client Secret。

## API 格式与默认地址

| 服务 | 可选格式 | Base URL 默认值 | 请求路径 | 鉴权 |
| --- | --- | --- | --- | --- |
| OpenAI | OpenAI Chat Completions | `https://api.openai.com/v1` | `POST /chat/completions` | `Authorization: Bearer ...` |
| Gemini | Gemini 原生 API | `https://generativelanguage.googleapis.com` | `POST /v1beta/models/{model}:generateContent` | `x-goog-api-key: ...` |
| Gemini | OpenAI 兼容 API | `https://generativelanguage.googleapis.com/v1beta/openai` | `POST /chat/completions` | `Authorization: Bearer ...` |
| Claude | Claude Messages API | `https://api.anthropic.com` | `POST /v1/messages` | `x-api-key: ...`、`anthropic-version` |
| Claude | OpenAI 兼容 API | `https://api.anthropic.com/v1` | `POST /chat/completions` | `Authorization: Bearer ...` |

Gemini 和 Claude 的旧配置在升级后默认继续使用原生格式。Claude 官方将 OpenAI SDK 兼容层定位为迁移、测试和模型对比入口；长期使用 Claude 时优先选择原生 Messages API，以保留完整的原生能力。

每家都可以覆盖 Base URL，适用于私有网关、中转服务或本地模型服务。自定义地址必须兼容当前选择的 API 格式。切换格式时，如果地址为空或仍是官方默认地址，设置页会自动换成新格式对应的默认地址；已经填写的自定义网关地址不会被覆盖。

Base URL 只填写 API 根地址，不要重复加入 `/chat/completions`、`:generateContent` 或 `/messages`。

## 配置键

```text
ai_provider

ai_openai_base_url
ai_openai_api_key
ai_openai_model

ai_gemini_base_url
ai_gemini_api_key
ai_gemini_model
ai_gemini_api_format

ai_claude_base_url
ai_claude_api_key
ai_claude_model
ai_claude_api_format

proxy_ai_enabled
```

`ai_provider` 只接受 `openai`、`gemini` 或 `claude`。

`ai_gemini_api_format` 和 `ai_claude_api_format` 只接受：

- `native`：供应商原生协议；
- `openai`：OpenAI Chat Completions 兼容协议。

旧版本的 `ai_base_url`、`ai_api_key` 和 `ai_model` 会继续作为 OpenAI 配置读取，升级后不需要立即重新录入。

## 读取模型与测试

每张供应商卡片都有两个操作：

1. **读取模型列表**：使用当前表单中的格式、Base URL 和 Key，不要求先保存，也不要求先填写模型；空白 Key 会沿用服务器已经保存的 Key。
2. **用 hi 测试连接**：点选或手动填写模型后，直接使用当前尚未保存的表单真实发送一条内容为 `hi` 的最小请求，限制短输出，不加载 AnimateTool 工具。

读取和测试都不会修改服务器配置。只有点击设置页顶部的“保存更改”，当前供应商、格式、地址、Key 和模型才会成为 AI 助手实际使用的配置。

Claude 选择 OpenAI 兼容格式且使用官方地址时，聊天测试走兼容入口，模型列表则使用 Anthropic 官方原生 `/v1/models` 鉴权方式；自定义网关仍按所选 OpenAI 兼容格式读取 `/models`。

测试成功会显示：

- 实际供应商；
- 模型 ID；
- 模型回复；
- 请求延迟；
- 检查时间。

测试失败会区分常见情况：

- `401 / 403`：Key 无效、权限不足或项目未启用 API；
- `404`：Base URL 或模型名称错误；
- `429`：限流、余额或额度不足；
- 超时：网络、代理或服务不可达；
- 空响应：接口已返回，但没有生成文本内容。

读取模型列表失败不一定代表聊天接口不可用；部分中转服务没有实现模型列表。最终以“用 hi 测试连接”的结果为准。

## 工具调用

原生格式下，AnimateTool 会把内部工具定义转换为各供应商的协议：

- OpenAI function tools；
- Gemini function declarations / function responses；
- Claude tool use / tool result。

使用 OpenAI 兼容格式时，则直接使用标准 function tools。订阅和系统查询等助手能力可以在两种格式下继续工作。

## 官方参考

- [OpenAI Chat Completions](https://platform.openai.com/docs/api-reference/chat)
- [Gemini generateContent](https://ai.google.dev/api/generate-content)
- [Gemini OpenAI compatibility](https://ai.google.dev/gemini-api/docs/openai)
- [Anthropic Messages API](https://platform.claude.com/docs/en/api/messages)
- [Claude OpenAI SDK compatibility](https://platform.claude.com/docs/en/api/openai-sdk)

!!! warning
    API Key 只保存在服务器配置中，不会回传到设置页。不要在聊天中粘贴密码、Token、完整配置文件或未脱敏日志。
