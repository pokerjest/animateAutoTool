package ai

import (
	"context"
	"fmt"
)

// ToolHandler is a function that executes a tool call.
type ToolHandler func(ctx context.Context, args string) (string, error)

// RegisteredTool holds the definition and execution logic of a tool.
type RegisteredTool struct {
	Definition Tool
	Handler    ToolHandler
}

// Registry manages the tools available to the AI.
type Registry struct {
	tools map[string]RegisteredTool
}

// NewRegistry creates a new empty Tool Registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]RegisteredTool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(name, description string, params any, handler ToolHandler) {
	r.tools[name] = RegisteredTool{
		Definition: Tool{
			Type: "function",
			Function: FunctionSchema{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		},
		Handler: handler,
	}
}

// GetToolDefinitions returns the schema for all registered tools, ready to be sent to the LLM.
func (r *Registry) GetToolDefinitions() []Tool {
	var defs []Tool
	for _, rt := range r.tools {
		defs = append(defs, rt.Definition)
	}
	return defs
}

// ExecuteTool runs a specific tool by name with the given JSON arguments.
func (r *Registry) ExecuteTool(ctx context.Context, name string, args string) (string, error) {
	tool, exists := r.tools[name]
	if !exists {
		return "", fmt.Errorf("tool '%s' not found", name)
	}

	result, err := tool.Handler(ctx, args)
	if err != nil {
		// Even if the tool errors, we often want to return the error to the LLM as a string
		// so it can decide how to handle it, but we also return the error.
		return fmt.Sprintf("Error executing tool: %v", err), err
	}

	return result, nil
}

// JSONSchemaObject is a helper to build simple object schemas.
func JSONSchemaObject(properties map[string]any, required []string) any {
	return &JSONSchemaHelper{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}

// JSONSchemaProperty is a helper to build property definitions.
func JSONSchemaProperty(propType string, description string) map[string]any {
	return map[string]any{
		"type":        propType,
		"description": description,
	}
}
