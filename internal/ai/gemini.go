package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/httpx"
)

type GeminiClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiFunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	Tools             []struct {
		FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
	GenerationConfig *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature     float32               `json:"temperature,omitempty"`
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
}

func NewGeminiClientWithProxy(baseURL, apiKey, model, proxyURL string) *GeminiClient {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	return &GeminiClient{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		model:      strings.TrimSpace(model),
		httpClient: httpx.NewHTTPClientWithProxy(60*time.Second, proxyURL),
	}
}

func (c *GeminiClient) apiRoot() string {
	if strings.HasSuffix(c.baseURL, "/v1beta") {
		return c.baseURL
	}
	return c.baseURL + "/v1beta"
}

func geminiToolResult(content string) map[string]any {
	var value any
	if json.Unmarshal([]byte(content), &value) == nil {
		if object, ok := value.(map[string]any); ok {
			return object
		}
		return map[string]any{"result": value}
	}
	return map[string]any{"result": content}
}

func appendGeminiContent(contents []geminiContent, role string, parts ...geminiPart) []geminiContent {
	if len(parts) == 0 {
		return contents
	}
	if len(contents) > 0 && contents[len(contents)-1].Role == role {
		contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, parts...)
		return contents
	}
	return append(contents, geminiContent{Role: role, Parts: parts})
}

// AnimateTool's internal schemas use additionalProperties=false so the local
// executor can reject unknown arguments. Gemini's native function declaration
// schema does not recognize that keyword, so remove it only from the
// provider-facing copy and keep the registry schema strict.
func geminiCompatibleSchema(schema any) any {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return schema
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return schema
	}
	return removeGeminiUnsupportedSchemaFields(value)
}

func removeGeminiUnsupportedSchemaFields(value any) any {
	switch current := value.(type) {
	case map[string]any:
		delete(current, "additionalProperties")
		for key, child := range current {
			current[key] = removeGeminiUnsupportedSchemaFields(child)
		}
	case []any:
		for index, child := range current {
			current[index] = removeGeminiUnsupportedSchemaFields(child)
		}
	}
	return value
}

func geminiThinkingLevel(modelName, requested string) string {
	modelName = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(modelName), "models/"))
	if !strings.HasPrefix(modelName, "gemini-3") {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(requested))
	default:
		return ""
	}
}

func toGeminiRequest(req ChatCompletionRequest, modelName string) geminiRequest {
	result := geminiRequest{}
	var systemParts []geminiPart
	for _, message := range req.Messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) != "" {
				systemParts = append(systemParts, geminiPart{Text: message.Content})
			}
		case "assistant":
			parts := make([]geminiPart, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				parts = append(parts, geminiPart{Text: message.Content})
			}
			for _, call := range message.ToolCalls {
				args := map[string]any{}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				parts = append(parts, geminiPart{
					FunctionCall:     &geminiFunctionCall{Name: call.Function.Name, Args: args},
					ThoughtSignature: call.ThoughtSignature,
				})
			}
			result.Contents = appendGeminiContent(result.Contents, "model", parts...)
		case "tool":
			result.Contents = appendGeminiContent(result.Contents, "user", geminiPart{
				FunctionResponse: &geminiFunctionResponse{Name: message.Name, Response: geminiToolResult(message.Content)},
			})
		default:
			if message.Content != "" {
				result.Contents = appendGeminiContent(result.Contents, "user", geminiPart{Text: message.Content})
			}
		}
	}
	if len(systemParts) > 0 {
		result.SystemInstruction = &geminiContent{Parts: systemParts}
	}
	if len(req.Tools) > 0 {
		toolGroup := struct {
			FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
		}{}
		for _, tool := range req.Tools {
			toolGroup.FunctionDeclarations = append(toolGroup.FunctionDeclarations, geminiFunctionDeclaration{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  geminiCompatibleSchema(tool.Function.Parameters),
			})
		}
		result.Tools = append(result.Tools, toolGroup)
	}
	thinkingLevel := geminiThinkingLevel(modelName, req.ThinkingLevel)
	if req.Temperature != 0 || req.MaxTokens != 0 || thinkingLevel != "" {
		result.GenerationConfig = &geminiGenerationConfig{
			Temperature: req.Temperature, MaxOutputTokens: req.MaxTokens,
		}
		if thinkingLevel != "" {
			result.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{ThinkingLevel: thinkingLevel}
		}
	}
	return result
}

func (c *GeminiClient) CreateChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	modelName := strings.TrimPrefix(strings.TrimSpace(req.Model), "models/")
	if modelName == "" {
		modelName = strings.TrimPrefix(c.model, "models/")
	}
	if modelName == "" {
		return nil, errors.New("gemini model is required")
	}
	bodyBytes, err := json.Marshal(toGeminiRequest(req, modelName))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Gemini request: %w", err)
	}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", c.apiRoot(), url.PathEscape(modelName))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(resp.Body)
		return nil, &ProviderError{StatusCode: resp.StatusCode, Body: string(errorBody)}
	}
	var apiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode Gemini response: %w", err)
	}
	if len(apiResp.Candidates) == 0 {
		return nil, errors.New("gemini returned no candidates")
	}
	message := ChatMessage{Role: "assistant"}
	for index, part := range apiResp.Candidates[0].Content.Parts {
		if part.Text != "" {
			message.Content += part.Text
		}
		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID:               fmt.Sprintf("gemini-call-%d", index),
				Type:             "function",
				Function:         ToolFunction{Name: part.FunctionCall.Name, Arguments: string(args)},
				ThoughtSignature: part.ThoughtSignature,
			})
		}
	}
	return &ChatCompletionResponse{
		Choices: []Choice{{Index: 0, Message: message, FinishReason: strings.ToLower(apiResp.Candidates[0].FinishReason)}},
	}, nil
}

func (c *GeminiClient) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiRoot()+"/models?pageSize=1000", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini models request: %w", err)
	}
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini models request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(resp.Body)
		return nil, &ProviderError{StatusCode: resp.StatusCode, Body: string(errorBody)}
	}
	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode Gemini models: %w", err)
	}
	models := make([]string, 0, len(payload.Models))
	for _, item := range payload.Models {
		supported := false
		for _, method := range item.SupportedGenerationMethods {
			if method == "generateContent" {
				supported = true
				break
			}
		}
		if supported {
			models = append(models, strings.TrimPrefix(item.Name, "models/"))
		}
	}
	return models, nil
}
