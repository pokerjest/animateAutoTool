package ai

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction contains the name and arguments of the function to call.
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// Tool represents a tool definition passed to the model.
type Tool struct {
	Type     string         `json:"type"` // always "function"
	Function FunctionSchema `json:"function"`
}

// FunctionSchema defines the signature of a tool function.
type FunctionSchema struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"` // JSON Schema object
}

// ChatCompletionRequest is the payload sent to the chat/completions endpoint.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []Tool        `json:"tools,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
}

// ChatCompletionResponse is the payload received from the chat/completions endpoint.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// JSONSchemaHelper is a basic structure to build JSON schemas for parameters.
type JSONSchemaHelper struct {
	Type       string         `json:"type"` // "object"
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// ModelListResponse represents the list of models returned by the /models endpoint.
type ModelListResponse struct {
	Data []ModelData `json:"data"`
}

type ModelData struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
