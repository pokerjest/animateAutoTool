package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// ToolHandler is a function that executes a tool call.
type ToolHandler func(ctx context.Context, args string) (string, error)

type ToolRisk string

const (
	ToolRiskRead    ToolRisk = "read"
	ToolRiskPropose ToolRisk = "propose"
	ToolRiskWrite   ToolRisk = "write"
)

var (
	ErrToolConfirmationRequired = errors.New("tool confirmation required")
	ErrToolCallLimit            = errors.New("tool call limit reached")
	ErrToolConcurrencyLimit     = errors.New("tool concurrency limit reached")
)

const (
	defaultToolConcurrency = 8
	defaultToolCallLimit   = 32
	toolCallWindow         = 2 * time.Minute
)

type ToolSpec struct {
	Name                 string
	Description          string
	InputSchema          any
	Risk                 ToolRisk
	RequiresConfirmation bool
	Timeout              time.Duration
	Handler              ToolHandler
}

type ToolExecutionMeta struct {
	RequestID  string
	TaskID     string
	SessionID  string
	ProposalID string
	UserID     uint
	Username   string
	Provider   string
	Model      string
}

type ToolRunEvent struct {
	Meta                  ToolExecutionMeta
	Name                  string
	Risk                  ToolRisk
	Arguments             string
	Result                string
	Error                 string
	Duration              time.Duration
	RequiresConfirmation  bool
	ConfirmationValidated bool
}

type ToolObserver func(ToolRunEvent)

type toolExecutionMetaKey struct{}
type toolCallBudgetKey struct{}

type toolCallWindowState struct {
	started time.Time
	count   int
}

func WithToolExecutionMeta(ctx context.Context, meta ToolExecutionMeta) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(toolCallBudgetKey{}).(*int); !ok {
		count := 0
		ctx = context.WithValue(ctx, toolCallBudgetKey{}, &count)
	}
	return context.WithValue(ctx, toolExecutionMetaKey{}, meta)
}

func ToolExecutionMetaFromContext(ctx context.Context) ToolExecutionMeta {
	if meta, ok := ctx.Value(toolExecutionMetaKey{}).(ToolExecutionMeta); ok {
		return meta
	}
	return ToolExecutionMeta{}
}

// RegisteredTool holds the definition and execution logic of a tool.
type RegisteredTool struct {
	Definition Tool
	Spec       ToolSpec
}

// Registry manages the tools available to the AI.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]RegisteredTool
	observer ToolObserver
	sem      chan struct{}
	callMu   sync.Mutex
	calls    map[string]toolCallWindowState
}

// NewRegistry creates a new empty Tool Registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]RegisteredTool),
		sem:   make(chan struct{}, defaultToolConcurrency),
		calls: make(map[string]toolCallWindowState),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(name, description string, params any, handler ToolHandler) {
	r.RegisterSpec(ToolSpec{
		Name:        name,
		Description: description,
		InputSchema: params,
		Risk:        ToolRiskRead,
		Handler:     handler,
	})
}

func (r *Registry) RegisterSpec(spec ToolSpec) {
	if spec.Risk == "" {
		spec.Risk = ToolRiskRead
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 30 * time.Second
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[spec.Name] = RegisteredTool{
		Spec: spec,
		Definition: Tool{
			Type: "function",
			Function: FunctionSchema{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  spec.InputSchema,
			},
		},
	}
}

func (r *Registry) SetObserver(observer ToolObserver) {
	r.mu.Lock()
	r.observer = observer
	r.mu.Unlock()
}

func (r *Registry) ToolSpec(name string) (ToolSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool.Spec, ok
}

func (r *Registry) tool(name string) (RegisteredTool, ToolObserver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, r.observer, ok
}

// RegisterWrite adds an internal mutation tool. Write tools are deliberately
// excluded from model-visible definitions and can only be invoked through
// ExecuteConfirmedTool after the application validates a one-time token.
func (r *Registry) RegisterWrite(name, description string, params any, handler ToolHandler) {
	r.RegisterSpec(ToolSpec{
		Name:                 name,
		Description:          description,
		InputSchema:          params,
		Risk:                 ToolRiskWrite,
		RequiresConfirmation: true,
		Handler:              handler,
	})
}

// RegisterProposal adds a model-visible tool that may create a persisted
// proposal but must not mutate the underlying business object.
func (r *Registry) RegisterProposal(name, description string, params any, handler ToolHandler) {
	r.RegisterSpec(ToolSpec{
		Name:        name,
		Description: description,
		InputSchema: params,
		Risk:        ToolRiskPropose,
		Handler:     handler,
	})
}

// Deprecated compatibility implementation retained for old call sites.
func (r *Registry) registerLegacy(name, description string, params any, handler ToolHandler) {
	r.tools[name] = RegisteredTool{
		Definition: Tool{
			Type: "function",
			Function: FunctionSchema{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		},
		Spec: ToolSpec{Name: name, Description: description, InputSchema: params, Risk: ToolRiskRead, Handler: handler},
	}
}

// GetToolDefinitions returns the schema for all registered tools, ready to be sent to the LLM.
func (r *Registry) GetToolDefinitions() []Tool {
	r.mu.RLock()
	var defs []Tool
	for _, rt := range r.tools {
		if rt.Spec.Risk == ToolRiskWrite {
			continue
		}
		defs = append(defs, rt.Definition)
	}
	r.mu.RUnlock()
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Function.Name < defs[j].Function.Name
	})
	return defs
}

// ExecuteTool runs a specific tool by name with the given JSON arguments.
func (r *Registry) ExecuteTool(ctx context.Context, name string, args string) (string, error) {
	return r.execute(ctx, name, args, false)
}

func (r *Registry) ExecuteConfirmedTool(ctx context.Context, name string, args string) (string, error) {
	return r.execute(ctx, name, args, true)
}

func (r *Registry) execute(ctx context.Context, name, args string, confirmed bool) (result string, resultErr error) {
	tool, observer, exists := r.tool(name)
	if !exists {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	started := time.Now()
	defer func() {
		if observer == nil {
			return
		}
		event := ToolRunEvent{
			Meta:                  ToolExecutionMetaFromContext(ctx),
			Name:                  name,
			Risk:                  tool.Spec.Risk,
			Arguments:             args,
			Result:                result,
			Duration:              time.Since(started),
			RequiresConfirmation:  tool.Spec.RequiresConfirmation,
			ConfirmationValidated: confirmed,
		}
		if resultErr != nil {
			event.Error = resultErr.Error()
		}
		observer(event)
	}()
	if tool.Spec.Risk == ToolRiskWrite && (!confirmed || !tool.Spec.RequiresConfirmation) {
		resultErr = ErrToolConfirmationRequired
		return "", resultErr
	}
	if !r.allowCall(ctx) {
		resultErr = ErrToolCallLimit
		return "", resultErr
	}
	if err := validateToolArguments(tool.Spec.InputSchema, args); err != nil {
		resultErr = fmt.Errorf("invalid tool arguments: %w", err)
		return "", resultErr
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	default:
		resultErr = ErrToolConcurrencyLimit
		return "", resultErr
	}
	runCtx, cancel := context.WithTimeout(ctx, tool.Spec.Timeout)
	defer cancel()
	result, resultErr = tool.Spec.Handler(runCtx, args)
	if resultErr != nil {
		// Even if the tool errors, we often want to return the error to the LLM as a string
		// so it can decide how to handle it, but we also return the error.
		return fmt.Sprintf("Error executing tool: %v", resultErr), resultErr
	}

	return result, nil
}

func (r *Registry) allowCall(ctx context.Context) bool {
	meta := ToolExecutionMetaFromContext(ctx)
	requestID := strings.TrimSpace(meta.RequestID)
	if requestID != "" {
		now := time.Now()
		r.callMu.Lock()
		defer r.callMu.Unlock()
		state := r.calls[requestID]
		if state.started.IsZero() || now.Sub(state.started) >= toolCallWindow {
			state = toolCallWindowState{started: now}
		}
		if state.count >= defaultToolCallLimit {
			r.calls[requestID] = state
			return false
		}
		state.count++
		r.calls[requestID] = state
		if len(r.calls) > 1024 {
			for key, item := range r.calls {
				if now.Sub(item.started) >= toolCallWindow {
					delete(r.calls, key)
				}
			}
		}
		return true
	}
	if counter, ok := ctx.Value(toolCallBudgetKey{}).(*int); ok && counter != nil {
		(*counter)++
		return *counter <= defaultToolCallLimit
	}
	return true
}

func validateToolArguments(schema any, raw string) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return errors.New("arguments must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("arguments contain trailing JSON content")
	}
	var schemaMap map[string]any
	encoded, err := json.Marshal(schema)
	if err != nil {
		return errors.New("tool schema is invalid")
	}
	if err := json.Unmarshal(encoded, &schemaMap); err != nil {
		return errors.New("tool schema is invalid")
	}
	return validateJSONSchemaValue(schemaMap, value, "arguments")
}

func validateJSONSchemaValue(schema map[string]any, value any, path string) error {
	expected, _ := schema["type"].(string)
	switch expected {
	case "object":
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		if additional, exists := schema["additionalProperties"]; exists && additional == false {
			for key := range object {
				if _, ok := properties[key]; !ok {
					return fmt.Errorf("unknown field %q", key)
				}
			}
		}
		if required, ok := schema["required"].([]any); ok {
			for _, item := range required {
				key, _ := item.(string)
				if key == "" {
					continue
				}
				field, exists := object[key]
				if !exists || field == nil {
					return fmt.Errorf("field %q is required", key)
				}
			}
		}
		for key, field := range object {
			property, ok := properties[key].(map[string]any)
			if !ok || field == nil {
				continue
			}
			if err := validateJSONSchemaValue(property, field, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		if itemSchema != nil {
			for index, item := range items {
				if err := validateJSONSchemaValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if minimum, ok := schema["minLength"].(float64); ok && float64(len([]rune(text))) < minimum {
			return fmt.Errorf("%s is shorter than the minimum length", path)
		}
		if maximum, ok := schema["maxLength"].(float64); ok && float64(len([]rune(text))) > maximum {
			return fmt.Errorf("%s is longer than the maximum length", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		if _, err := number.Int64(); err != nil {
			return fmt.Errorf("%s must be an integer", path)
		}
		if err := validateJSONSchemaNumber(schema, number, path); err != nil {
			return err
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		if _, err := number.Float64(); err != nil {
			return fmt.Errorf("%s must be a number", path)
		}
		if err := validateJSONSchemaNumber(schema, number, path); err != nil {
			return err
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	}
	if enumValues, ok := schema["enum"].([]any); ok && len(enumValues) > 0 {
		for _, candidate := range enumValues {
			if fmt.Sprint(candidate) == fmt.Sprint(value) {
				return nil
			}
		}
		return fmt.Errorf("%s is not an allowed value", path)
	}
	return nil
}

func validateJSONSchemaNumber(schema map[string]any, number json.Number, path string) error {
	value, err := number.Float64()
	if err != nil {
		return fmt.Errorf("%s must be a number", path)
	}
	if minimum, ok := schema["minimum"].(float64); ok && value < minimum {
		return fmt.Errorf("%s is below the minimum", path)
	}
	if maximum, ok := schema["maximum"].(float64); ok && value > maximum {
		return fmt.Errorf("%s is above the maximum", path)
	}
	return nil
}

// JSONSchemaObject is a helper to build simple object schemas.
func JSONSchemaObject(properties map[string]any, required []string) any {
	return &JSONSchemaHelper{
		Type:                 "object",
		Properties:           properties,
		Required:             required,
		AdditionalProperties: false,
	}
}

// JSONSchemaProperty is a helper to build property definitions.
func JSONSchemaProperty(propType string, description string) map[string]any {
	return map[string]any{
		"type":        propType,
		"description": description,
	}
}
