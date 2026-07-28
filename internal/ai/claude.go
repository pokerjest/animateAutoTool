package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/httpx"
)

type ClaudeClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type claudeMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type claudeRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	System      string          `json:"system,omitempty"`
	Messages    []claudeMessage `json:"messages"`
	Tools       []claudeTool    `json:"tools,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
}

type claudeResponse struct {
	ID         string               `json:"id"`
	Content    []claudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
}

func NewClaudeClientWithProxy(baseURL, apiKey, model, proxyURL string) *ClaudeClient {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &ClaudeClient{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		model:      strings.TrimSpace(model),
		httpClient: httpx.NewHTTPClientWithProxy(60*time.Second, proxyURL),
	}
}

func (c *ClaudeClient) apiRoot() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL
	}
	return c.baseURL + "/v1"
}

func appendClaudeMessage(messages []claudeMessage, role string, blocks ...claudeContentBlock) []claudeMessage {
	if len(blocks) == 0 {
		return messages
	}
	if len(messages) > 0 && messages[len(messages)-1].Role == role {
		messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, blocks...)
		return messages
	}
	return append(messages, claudeMessage{Role: role, Content: blocks})
}

func toClaudeRequest(req ChatCompletionRequest, fallbackModel string) claudeRequest {
	result := claudeRequest{Model: strings.TrimSpace(req.Model), MaxTokens: req.MaxTokens, Temperature: req.Temperature}
	if result.Model == "" {
		result.Model = fallbackModel
	}
	if result.MaxTokens <= 0 {
		result.MaxTokens = 2048
	}
	var systems []string
	for _, message := range req.Messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) != "" {
				systems = append(systems, message.Content)
			}
		case "assistant":
			blocks := make([]claudeContentBlock, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				blocks = append(blocks, claudeContentBlock{Type: "text", Text: message.Content})
			}
			for _, call := range message.ToolCalls {
				input := json.RawMessage(call.Function.Arguments)
				if !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				blocks = append(blocks, claudeContentBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: input})
			}
			result.Messages = appendClaudeMessage(result.Messages, "assistant", blocks...)
		case "tool":
			result.Messages = appendClaudeMessage(result.Messages, "user", claudeContentBlock{
				Type: "tool_result", ToolUseID: message.ToolCallID, Content: message.Content,
			})
		default:
			if message.Content != "" {
				result.Messages = appendClaudeMessage(result.Messages, "user", claudeContentBlock{Type: "text", Text: message.Content})
			}
		}
	}
	result.System = strings.Join(systems, "\n\n")
	for _, tool := range req.Tools {
		result.Tools = append(result.Tools, claudeTool{
			Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: tool.Function.Parameters,
		})
	}
	return result
}

func (c *ClaudeClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}
}

func (c *ClaudeClient) CreateChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	payload := toClaudeRequest(req, c.model)
	if payload.Model == "" {
		return nil, errors.New("claude model is required")
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Claude request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiRoot()+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create Claude request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, NewProviderErrorFromResponse(resp)
	}
	var apiResp claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode Claude response: %w", err)
	}
	message := ChatMessage{Role: "assistant"}
	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			message.Content += block.Text
		case "tool_use":
			args := string(block.Input)
			if args == "" {
				args = "{}"
			}
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID: block.ID, Type: "function", Function: ToolFunction{Name: block.Name, Arguments: args},
			})
		}
	}
	if message.Content == "" && len(message.ToolCalls) == 0 {
		return nil, errors.New("claude returned empty content")
	}
	return &ChatCompletionResponse{
		ID:      apiResp.ID,
		Choices: []Choice{{Index: 0, Message: message, FinishReason: apiResp.StopReason}},
	}, nil
}

func (c *ClaudeClient) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiRoot()+"/models?limit=1000", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Claude models request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude models request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, NewProviderErrorFromResponse(resp)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode Claude models: %w", err)
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			models = append(models, item.ID)
		}
	}
	return models, nil
}
