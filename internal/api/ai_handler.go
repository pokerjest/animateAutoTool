package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

var (
	globalChatHistory []ai.ChatMessage
	chatMutex         sync.Mutex
)

// AIChatHandler processes incoming messages from the AI chat widget.
func AIChatHandler(c *gin.Context) {
	userMessage := c.PostForm("message")
	if strings.TrimSpace(userMessage) == "" {
		// Just render an empty string if empty
		c.String(http.StatusOK, "")
		return
	}

	// 1. Get Settings
	apiKey := configValue(model.ConfigKeyAIApiKey)
	baseURL := configValue(model.ConfigKeyAIBaseURL)
	modelName := configValue(model.ConfigKeyAIModel)

	if apiKey == "" {
		c.Data(http.StatusOK, "text/html", []byte(chatBubble("assistant", "您好！请先在设置页配置 AI 的 API Key 和 Base URL。")))
		return
	}
	if modelName == "" {
		modelName = "gpt-4o-mini" // fallback
	}

	client := ai.NewClient(baseURL, apiKey, modelName)
	tools := GlobalAIRegistry.GetToolDefinitions()

	chatMutex.Lock()
	defer chatMutex.Unlock()

	// Initialize history if empty
	if len(globalChatHistory) == 0 {
		globalChatHistory = append(globalChatHistory, ai.ChatMessage{
			Role:    "system",
			Content: "You are a helpful assistant integrated into AnimateAutoTool, an anime downloading and management application. Use tools to help the user manage their system. Be concise.",
		})
	}

	// Add user message
	globalChatHistory = append(globalChatHistory, ai.ChatMessage{
		Role:    "user",
		Content: userMessage,
	})

	// Render the user message immediately in the response, along with the "thinking" or final response
	// Actually, HTMX usually expects the appended content. If the user form clears, we just return the newly added messages.
	// But let's build the HTML string to return: User Bubble + Assistant Bubble.
	// 2. Call LLM Loop (handle tool calls)
	var responseHTML strings.Builder
	for {
		req := ai.ChatCompletionRequest{
			Model:    modelName,
			Messages: globalChatHistory,
			Tools:    tools,
		}

		resp, err := client.CreateChatCompletion(context.Background(), req)
		if err != nil {
			log.Printf("AI API Error: %v", err)
			msg := "抱歉，调用大模型接口失败，请检查设置中的 Base URL 和 API Key 或网络连通性。"
			globalChatHistory = append(globalChatHistory, ai.ChatMessage{Role: "assistant", Content: msg})
			responseHTML.WriteString(chatBubble("assistant", msg))
			break
		}

		choice := resp.Choices[0].Message

		// Append assistant message to history
		globalChatHistory = append(globalChatHistory, choice)

		if len(choice.ToolCalls) == 0 {
			// Normal reply
			content := choice.Content
			if content == "" {
				content = "执行完毕。"
			}
			responseHTML.WriteString(chatBubble("assistant", content))
			break
		}

		// Execute tools
		for _, toolCall := range choice.ToolCalls {
			log.Printf("AI Assistant executing tool: %s", toolCall.Function.Name)
			resultStr, err := GlobalAIRegistry.ExecuteTool(context.Background(), toolCall.Function.Name, toolCall.Function.Arguments)
			if err != nil {
				log.Printf("Tool error: %v", err)
			}

			// Return result to LLM
			globalChatHistory = append(globalChatHistory, ai.ChatMessage{
				Role:       "tool",
				ToolCallID: toolCall.ID,
				Name:       toolCall.Function.Name,
				Content:    resultStr,
			})
		}
	}

	c.Data(http.StatusOK, "text/html", []byte(responseHTML.String()))
}

// AIClearHistoryHandler clears the chat context.
func AIClearHistoryHandler(c *gin.Context) {
	chatMutex.Lock()
	globalChatHistory = nil
	chatMutex.Unlock()
	c.Data(http.StatusOK, "text/html", []byte(chatBubble("assistant", "对话历史已清空。")))
}

// chatBubble renders a premium Gemini-style HTML chat bubble for HTMX insertion
func chatBubble(role, content string) string {
	// Convert newlines to <br> for HTML display
	content = strings.ReplaceAll(content, "\n", "<br>")

	if role == "user" {
		return fmt.Sprintf(`
		<div class="flex justify-end w-full mb-8">
			<div class="bg-gray-100 text-gray-800 rounded-2xl px-4 py-3 max-w-[85%%] text-[15px]">
				%s
			</div>
		</div>`, content)
	}

	// Assistant bubble
	return fmt.Sprintf(`
	<div class="w-full mb-8">
		<div class="flex items-center gap-2 mb-2">
			<div class="h-6 w-6 rounded-full bg-blue-50 flex items-center justify-center text-blue-600">
				<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="currentColor">
					<path d="M12 2L14.5 9L21.5 11.5L14.5 14L12 21L9.5 14L2.5 11.5L9.5 9L12 2Z"/>
				</svg>
			</div>
			<span class="text-xs font-bold text-gray-500 uppercase tracking-wider">AI 助手</span>
		</div>
		<div class="text-[15px] leading-relaxed text-gray-700 pl-8">
			%s
		</div>
	</div>`, content)
}

// AIConfigHandler handles saving AI settings
func AIConfigHandler(c *gin.Context) {
	baseUrl := c.PostForm("ai_base_url")
	apiKey := c.PostForm("ai_api_key")
	modelName := c.PostForm("ai_model")

	if err := db.SaveGlobalConfig(model.ConfigKeyAIBaseURL, baseUrl); err != nil {
		jsonServerError(c, "保存 AI Base URL", err)
		return
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyAIApiKey, apiKey); err != nil {
		jsonServerError(c, "保存 AI API Key", err)
		return
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyAIModel, modelName); err != nil {
		jsonServerError(c, "保存 AI 模型", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "AI 设置已保存"})
}

// GetAIStatusHandler gets the current AI config state for the settings page
func GetAIStatusHandler(c *gin.Context) {
	baseUrl := configValue(model.ConfigKeyAIBaseURL)
	apiKey := configValue(model.ConfigKeyAIApiKey)
	modelName := configValue(model.ConfigKeyAIModel)

	if baseUrl == "" {
		baseUrl = "https://api.openai.com/v1"
	}
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url": baseUrl,
		"has_key":  apiKey != "",
		"model":    modelName,
	})
}

// GetAIModelsHandler fetches available models from the provider.
func GetAIModelsHandler(c *gin.Context) {
	baseURL := c.Query("base_url")
	apiKey := c.Query("api_key")

	if baseURL == "" {
		baseURL = configValue(model.ConfigKeyAIBaseURL)
	}
	if apiKey == "" {
		apiKey = configValue(model.ConfigKeyAIApiKey)
	}

	if apiKey == "" {
		c.JSON(http.StatusOK, gin.H{"models": []string{}})
		return
	}

	client := ai.NewClient(baseURL, apiKey, "")
	models, err := client.ListModels(context.Background())
	if err != nil {
		log.Printf("Failed to list models: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"models": models})
}
