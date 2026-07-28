package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateChatHistoryShortHistoryUnchanged(t *testing.T) {
	initial := []ai.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}

	assert.Equal(t, initial, truncateChatHistory(initial), "history under 25 messages should not be truncated")
}

func TestTruncateChatHistoryDropsOldestPreservesSystem(t *testing.T) {
	// 1 system + 40 alternating user/assistant turns = 41 messages, well over 25.
	msgs := []ai.ChatMessage{{Role: "system", Content: "sys"}}
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			ai.ChatMessage{Role: "user", Content: fmt.Sprintf("u%d", i)},
			ai.ChatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	truncated := truncateChatHistory(msgs)

	assert.LessOrEqual(t, len(truncated), 26,
		"after truncation should keep ~25 messages plus the system prompt")
	assert.Equal(t, "system", truncated[0].Role,
		"system prompt must be preserved at index 0")
	assert.Equal(t, "user", truncated[1].Role,
		"truncation should snap to a user turn so the dialogue starts cleanly")
}

func TestTruncateChatHistoryWithoutSystemPrompt(t *testing.T) {
	msgs := []ai.ChatMessage{}
	for i := 0; i < 40; i++ {
		msgs = append(msgs,
			ai.ChatMessage{Role: "user", Content: fmt.Sprintf("u%d", i)},
			ai.ChatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	truncated := truncateChatHistory(msgs)

	assert.LessOrEqual(t, len(truncated), 25,
		"without system message, history should be capped at maxMessages")
	assert.Equal(t, "user", truncated[0].Role,
		"truncated tail should still begin on a user turn")
}

func TestChatBubbleEscapesHTML(t *testing.T) {
	html := chatBubble("assistant", `<img src=x onerror=alert(1)>`+"\nnext")

	assert.NotContains(t, html, `<img src=x onerror=alert(1)>`)
	assert.Contains(t, html, `&lt;img src=x onerror=alert(1)&gt;`)
	assert.True(t, strings.Contains(html, "<br>next"), "newlines should remain visible as line breaks")
}

func TestVisibleAssistantMessagesExcludesInternalConversationState(t *testing.T) {
	history := []ai.ChatMessage{
		{Role: "system", Content: "secret system prompt"},
		{Role: "user", Content: "  帮我检查系统，api_key=secret-user-key  "},
		{
			Role:    "assistant",
			Content: "我先读取状态",
			ToolCalls: []ai.ToolCall{{
				ID: "call-1",
				Function: ai.ToolFunction{
					Name:      "get_system_status",
					Arguments: `{"token":"secret"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call-1", Content: `{"api_key":"secret","healthy":true}`},
		{Role: "assistant", Content: "系统当前运行正常，token=secret-assistant-token。"},
		{Role: "assistant", Content: "   "},
	}

	assert.Equal(t, []v1AssistantMessage{
		{Role: "user", Content: "帮我检查系统，api_key=[REDACTED]"},
		{Role: "assistant", Content: "系统当前运行正常，token=[REDACTED]"},
	}, visibleAssistantMessages(history))
}

func TestV1AITestHandlerSendsRealHiMessage(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
		_, _ = w.Write([]byte(`{"id":"chat-1","choices":[{"index":0,"message":{"role":"assistant","content":"Hi!"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai/test", bytes.NewBufferString(fmt.Sprintf(
		`{"provider":"openai","base_url":%q,"api_key":"test-key","model":"test-model"}`, server.URL,
	)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	V1AITestHandler(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, requestBody, `"content":"Reply with exactly: hi"`)
	assert.Contains(t, requestBody, `"max_tokens":256`)
	assert.Contains(t, recorder.Body.String(), `"connected":true`)
	assert.Contains(t, recorder.Body.String(), `"reply":"Hi!"`)
	assert.NotContains(t, recorder.Body.String(), "test-key")
}

func TestV1AITestHandlerUsesLowThinkingForGemini3(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai/test", bytes.NewBufferString(fmt.Sprintf(
		`{"provider":"gemini","format":"native","base_url":%q,"api_key":"test-key","model":"gemini-3.6-flash"}`, server.URL,
	)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	V1AITestHandler(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, requestBody, `"maxOutputTokens":256`)
	assert.Contains(t, requestBody, `"thinkingConfig":{"thinkingLevel":"low"}`)
	assert.Contains(t, recorder.Body.String(), `"connected":true`)
	assert.Contains(t, recorder.Body.String(), `"reply":"hi"`)
}

func TestV1AITestHandlerExplainsThinkingTokenExhaustion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"MAX_TOKENS"}]}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai/test", bytes.NewBufferString(fmt.Sprintf(
		`{"provider":"gemini","format":"native","base_url":%q,"api_key":"test-key","model":"gemini-3.6-flash"}`, server.URL,
	)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	V1AITestHandler(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"connected":false`)
	assert.Contains(t, recorder.Body.String(), "输出令牌被思考过程耗尽")
}

func TestV1AITestHandlerUsesSelectedOpenAICompatibilityFormat(t *testing.T) {
	for _, provider := range []string{"gemini", "claude"} {
		t.Run(provider, func(t *testing.T) {
			var requestPath, authorization string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestPath = r.URL.Path
				authorization = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(`{"id":"chat-1","choices":[{"index":0,"message":{"role":"assistant","content":"Hi!"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai/test", bytes.NewBufferString(fmt.Sprintf(
				`{"provider":%q,"format":"openai","base_url":%q,"api_key":"test-key","model":"test-model"}`, provider, server.URL,
			)))
			ctx.Request.Header.Set("Content-Type", "application/json")

			V1AITestHandler(ctx)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Equal(t, "/chat/completions", requestPath)
			assert.Equal(t, "Bearer test-key", authorization)
			assert.Contains(t, recorder.Body.String(), `"format":"openai"`)
			assert.Contains(t, recorder.Body.String(), `"connected":true`)
		})
	}
}

func TestV1AIModelsPostHandlerUsesUnsavedSettingsWithoutModel(t *testing.T) {
	var requestPath, apiKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		apiKey = r.Header.Get("x-goog-api-key")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-test","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/ai/models", bytes.NewBufferString(fmt.Sprintf(
		`{"provider":"gemini","format":"native","base_url":%q,"api_key":"unsaved-key","model":""}`, server.URL,
	)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	V1AIModelsPostHandler(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "/v1beta/models", requestPath)
	assert.Equal(t, "unsaved-key", apiKey)
	assert.Contains(t, recorder.Body.String(), `"gemini-test"`)
}

func TestClaudeOfficialCompatibilityUsesNativeModelListing(t *testing.T) {
	settings := aiProviderSettings{
		Provider: ai.ProviderClaude,
		Format:   ai.ProviderFormatOpenAI,
		BaseURL:  "https://api.anthropic.com/v1/",
	}
	modelSettings := modelListAIProviderSettings(settings)

	assert.Equal(t, ai.ProviderFormatNative, modelSettings.Format)
	assert.Equal(t, "https://api.anthropic.com", modelSettings.BaseURL)

	customSettings := settings
	customSettings.BaseURL = "https://claude-gateway.example.test/v1"
	assert.Equal(t, customSettings, modelListAIProviderSettings(customSettings))
}
