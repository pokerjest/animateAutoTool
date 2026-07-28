package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiClientUsesNativeProtocolAndConvertsToolCalls(t *testing.T) {
	var capturedHeader string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-test:generateContent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		capturedHeader = r.Header.Get("x-goog-api-key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"checking"},{"functionCall":{"name":"list_items","args":{"limit":2}},"thoughtSignature":"sig-native-tool"}]},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()

	client := NewGeminiClientWithProxy(srv.URL, "gemini-secret", "gemini-test", "")
	response, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "system", Content: "system prompt"}, {Role: "user", Content: "show items"}},
		Tools: []Tool{{Type: "function", Function: FunctionSchema{
			Name: "list_items", Description: "List items", Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		}}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion: %v", err)
	}
	if capturedHeader != "gemini-secret" {
		t.Fatalf("x-goog-api-key = %q", capturedHeader)
	}
	if capturedBody["systemInstruction"] == nil || capturedBody["tools"] == nil {
		t.Fatalf("native Gemini request did not include system/tools: %#v", capturedBody)
	}
	tools, ok := capturedBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("native Gemini tools are invalid: %#v", capturedBody["tools"])
	}
	toolGroup, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("native Gemini tool group is invalid: %#v", tools[0])
	}
	declarations, ok := toolGroup["functionDeclarations"].([]any)
	if !ok || len(declarations) != 1 {
		t.Fatalf("native Gemini declarations are invalid: %#v", toolGroup)
	}
	declaration, ok := declarations[0].(map[string]any)
	if !ok {
		t.Fatalf("native Gemini declaration is invalid: %#v", declarations[0])
	}
	parameters, ok := declaration["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("native Gemini parameters are invalid: %#v", declaration)
	}
	if _, exists := parameters["additionalProperties"]; exists {
		t.Fatalf("native Gemini request must omit unsupported additionalProperties: %#v", parameters)
	}
	message := response.Choices[0].Message
	if message.Content != "checking" || len(message.ToolCalls) != 1 {
		t.Fatalf("unexpected normalized response: %+v", message)
	}
	if message.ToolCalls[0].Function.Name != "list_items" || !strings.Contains(message.ToolCalls[0].Function.Arguments, `"limit":2`) {
		t.Fatalf("unexpected tool call: %+v", message.ToolCalls[0])
	}
	if message.ToolCalls[0].ThoughtSignature != "sig-native-tool" {
		t.Fatalf("thought signature = %q", message.ToolCalls[0].ThoughtSignature)
	}
}

func TestGeminiCompatibleSchemaRemovesUnsupportedFieldsRecursively(t *testing.T) {
	input := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
				},
			},
		},
	}

	converted, ok := geminiCompatibleSchema(input).(map[string]any)
	if !ok {
		t.Fatalf("converted schema has unexpected type")
	}
	if _, exists := converted["additionalProperties"]; exists {
		t.Fatalf("root additionalProperties was not removed: %#v", converted)
	}
	properties := converted["properties"].(map[string]any)
	items := properties["items"].(map[string]any)["items"].(map[string]any)
	if _, exists := items["additionalProperties"]; exists {
		t.Fatalf("nested additionalProperties was not removed: %#v", items)
	}
	if _, exists := input["additionalProperties"]; !exists {
		t.Fatalf("provider conversion mutated the registry schema")
	}
}

func TestGeminiClientAppliesThinkingLevelOnlyToGemini3(t *testing.T) {
	tests := []struct {
		model              string
		wantThinkingConfig bool
	}{
		{model: "gemini-3.6-flash", wantThinkingConfig: true},
		{model: "gemini-2.5-flash", wantThinkingConfig: false},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			var capturedBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &capturedBody)
				_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`))
			}))
			defer srv.Close()

			client := NewGeminiClientWithProxy(srv.URL, "key", test.model, "")
			_, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
				Messages:      []ChatMessage{{Role: "user", Content: "hi"}},
				MaxTokens:     256,
				ThinkingLevel: "low",
			})
			if err != nil {
				t.Fatalf("CreateChatCompletion: %v", err)
			}
			generationConfig, ok := capturedBody["generationConfig"].(map[string]any)
			if !ok {
				t.Fatalf("missing generationConfig: %#v", capturedBody)
			}
			_, hasThinkingConfig := generationConfig["thinkingConfig"]
			if hasThinkingConfig != test.wantThinkingConfig {
				t.Fatalf("thinkingConfig present = %t, want %t: %#v", hasThinkingConfig, test.wantThinkingConfig, generationConfig)
			}
		})
	}
}

func TestGeminiClientPreservesThoughtSignatureAcrossToolRoundTrip(t *testing.T) {
	requestCount := 0
	var secondRequest map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, _ := io.ReadAll(r.Body)
		if requestCount == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"list_items","args":{"limit":2}},"thoughtSignature":"sig-round-trip"}]},"finishReason":"STOP"}]}`))
			return
		}
		_ = json.Unmarshal(body, &secondRequest)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()

	client := NewGeminiClientWithProxy(srv.URL, "key", "gemini-3.6-flash", "")
	first, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "show items"}},
	})
	if err != nil {
		t.Fatalf("first CreateChatCompletion: %v", err)
	}
	assistantMessage := first.Choices[0].Message
	_, err = client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "show items"},
			assistantMessage,
			{Role: "tool", Name: "list_items", Content: `{"items":[]}`},
		},
	})
	if err != nil {
		t.Fatalf("second CreateChatCompletion: %v", err)
	}

	contents, ok := secondRequest["contents"].([]any)
	if !ok || len(contents) < 2 {
		t.Fatalf("unexpected contents: %#v", secondRequest["contents"])
	}
	modelContent, ok := contents[1].(map[string]any)
	if !ok {
		t.Fatalf("unexpected model content: %#v", contents[1])
	}
	parts, ok := modelContent["parts"].([]any)
	if !ok || len(parts) == 0 {
		t.Fatalf("unexpected model parts: %#v", modelContent["parts"])
	}
	functionPart, ok := parts[0].(map[string]any)
	if !ok || functionPart["thoughtSignature"] != "sig-round-trip" {
		t.Fatalf("thought signature was not preserved: %#v", parts[0])
	}
}

func TestGeminiClientListsGenerateContentModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-a","supportedGenerationMethods":["generateContent"]},{"name":"models/embed-a","supportedGenerationMethods":["embedContent"]}]}`))
	}))
	defer srv.Close()

	models, err := NewGeminiClientWithProxy(srv.URL, "key", "", "").ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0] != "gemini-a" {
		t.Fatalf("unexpected models: %v", models)
	}
}

func TestClaudeClientUsesMessagesProtocolAndConvertsToolCalls(t *testing.T) {
	var capturedAPIKey, capturedVersion string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		capturedAPIKey = r.Header.Get("x-api-key")
		capturedVersion = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		_, _ = w.Write([]byte(`{"id":"msg_1","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"tool_1","name":"list_items","input":{"limit":2}}],"stop_reason":"tool_use"}`))
	}))
	defer srv.Close()

	client := NewClaudeClientWithProxy(srv.URL, "claude-secret", "claude-test", "")
	response, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "system", Content: "system prompt"}, {Role: "user", Content: "show items"}},
		Tools: []Tool{{Type: "function", Function: FunctionSchema{
			Name: "list_items", Description: "List items", Parameters: map[string]any{"type": "object"},
		}}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion: %v", err)
	}
	if capturedAPIKey != "claude-secret" || capturedVersion != "2023-06-01" {
		t.Fatalf("unexpected headers: key=%q version=%q", capturedAPIKey, capturedVersion)
	}
	if capturedBody["system"] != "system prompt" || capturedBody["tools"] == nil {
		t.Fatalf("native Claude request did not include system/tools: %#v", capturedBody)
	}
	message := response.Choices[0].Message
	if message.Content != "checking" || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "tool_1" {
		t.Fatalf("unexpected normalized response: %+v", message)
	}
}

func TestClaudeClientListsModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-a"},{"id":"claude-b"}]}`))
	}))
	defer srv.Close()

	models, err := NewClaudeClientWithProxy(srv.URL, "key", "", "").ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 || models[1] != "claude-b" {
		t.Fatalf("unexpected models: %v", models)
	}
}

func TestProviderFactoryUsesOpenAICompatibilityForGeminiAndClaude(t *testing.T) {
	for _, provider := range []string{ProviderGemini, ProviderClaude} {
		t.Run(provider, func(t *testing.T) {
			var capturedPath, capturedAuthorization string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				capturedAuthorization = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(`{"id":"chat-1","choices":[{"index":0,"message":{"role":"assistant","content":"Hi!"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()

			client, err := NewProviderClient(ProviderConfig{
				Provider: provider,
				Format:   ProviderFormatOpenAI,
				BaseURL:  srv.URL,
				APIKey:   "compat-secret",
				Model:    "compat-model",
			})
			if err != nil {
				t.Fatalf("NewProviderClient: %v", err)
			}
			response, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			})
			if err != nil {
				t.Fatalf("CreateChatCompletion: %v", err)
			}
			if capturedPath != "/chat/completions" || capturedAuthorization != "Bearer compat-secret" {
				t.Fatalf("unexpected compatibility request: path=%q authorization=%q", capturedPath, capturedAuthorization)
			}
			if response.Choices[0].Message.Content != "Hi!" {
				t.Fatalf("unexpected response: %+v", response)
			}
		})
	}
}

func TestNormalizeProviderFormatDefaultsAndRejectsUnsupportedValues(t *testing.T) {
	tests := []struct {
		provider string
		format   string
		want     string
		wantErr  bool
	}{
		{ProviderOpenAI, "", ProviderFormatOpenAI, false},
		{ProviderGemini, "", ProviderFormatNative, false},
		{ProviderClaude, "", ProviderFormatNative, false},
		{ProviderGemini, ProviderFormatOpenAI, ProviderFormatOpenAI, false},
		{ProviderClaude, ProviderFormatOpenAI, ProviderFormatOpenAI, false},
		{ProviderOpenAI, ProviderFormatNative, "", true},
		{ProviderGemini, "oauth", "", true},
	}
	for _, test := range tests {
		got, err := NormalizeProviderFormat(test.provider, test.format)
		if test.wantErr {
			if err == nil {
				t.Fatalf("NormalizeProviderFormat(%q, %q) expected error", test.provider, test.format)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("NormalizeProviderFormat(%q, %q) = %q, %v; want %q", test.provider, test.format, got, err, test.want)
		}
	}
}
