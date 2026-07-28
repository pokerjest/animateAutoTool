package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const writeToolAppliedResult = "applied"

func TestRegistryRegisterAndExecute(t *testing.T) {
	r := NewRegistry()
	r.Register("echo", "Echoes the input string.",
		JSONSchemaObject(map[string]any{
			"text": JSONSchemaProperty("string", "text to echo"),
		}, []string{"text"}),
		func(ctx context.Context, args string) (string, error) {
			return args, nil
		},
	)
	defs := r.GetToolDefinitions()
	if len(defs) != 1 || defs[0].Type != "function" || defs[0].Function.Name != "echo" {
		t.Fatalf("unexpected definitions: %+v", defs)
	}
	got, err := r.ExecuteTool(context.Background(), "echo", `{"text":"hi"}`)
	if err != nil || got != `{"text":"hi"}` {
		t.Fatalf("unexpected execution result %q: %v", got, err)
	}
}

func TestRegistryExecuteToolMissing(t *testing.T) {
	r := NewRegistry()
	if _, err := r.ExecuteTool(context.Background(), "missing", "{}"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRegistryExecuteToolHandlerError(t *testing.T) {
	r := NewRegistry()
	wantErr := errors.New("boom")
	r.Register("explode", "Always errors.", nil, func(context.Context, string) (string, error) {
		return "", wantErr
	})
	out, err := r.ExecuteTool(context.Background(), "explode", "{}")
	if !errors.Is(err, wantErr) || !strings.Contains(out, "boom") {
		t.Fatalf("unexpected handler error result %q: %v", out, err)
	}
}

func TestJSONSchemaHelpers(t *testing.T) {
	prop := JSONSchemaProperty("string", "an episode title")
	if prop["type"] != "string" || prop["description"] != "an episode title" {
		t.Fatalf("unexpected property: %+v", prop)
	}
	schema, ok := JSONSchemaObject(map[string]any{"title": prop}, []string{"title"}).(*JSONSchemaHelper)
	if !ok || schema.Type != "object" || len(schema.Required) != 1 || schema.Required[0] != "title" {
		t.Fatalf("unexpected schema: %#v", schema)
	}
}

func TestRegistryHidesWriteToolsFromModelDefinitions(t *testing.T) {
	registry := NewRegistry()
	registry.Register("read_status", "read", JSONSchemaObject(map[string]any{}, nil), func(context.Context, string) (string, error) {
		return "ok", nil
	})
	registry.RegisterProposal("preview_change", "proposal", JSONSchemaObject(map[string]any{}, nil), func(context.Context, string) (string, error) {
		return "preview", nil
	})
	registry.RegisterWrite("apply_change", "write", JSONSchemaObject(map[string]any{}, nil), func(context.Context, string) (string, error) {
		return writeToolAppliedResult, nil
	})

	definitions := registry.GetToolDefinitions()
	if len(definitions) != 2 {
		t.Fatalf("expected two model-visible tools, got %d", len(definitions))
	}
	names := []string{definitions[0].Function.Name, definitions[1].Function.Name}
	if strings.Contains(strings.Join(names, ","), "apply_change") {
		t.Fatalf("write tool leaked into model definitions: %v", names)
	}
}

func TestRegistryWriteToolRequiresConfirmedExecution(t *testing.T) {
	registry := NewRegistry()
	called := false
	registry.RegisterWrite("apply_change", "write", JSONSchemaObject(map[string]any{
		"id": JSONSchemaProperty("integer", "resource ID"),
	}, []string{"id"}), func(context.Context, string) (string, error) {
		called = true
		return writeToolAppliedResult, nil
	})

	if _, err := registry.ExecuteTool(context.Background(), "apply_change", `{"id":1}`); !errors.Is(err, ErrToolConfirmationRequired) {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if called {
		t.Fatal("write handler ran without confirmed execution")
	}
	result, err := registry.ExecuteConfirmedTool(context.Background(), "apply_change", `{"id":1}`)
	if err != nil {
		t.Fatalf("confirmed execution failed: %v", err)
	}
	if result != writeToolAppliedResult || !called {
		t.Fatalf("unexpected confirmed result %q called=%v", result, called)
	}
}

func TestRegistryStrictlyValidatesToolArguments(t *testing.T) {
	registry := NewRegistry()
	registry.Register("inspect", "read", JSONSchemaObject(map[string]any{
		"id":     JSONSchemaProperty("integer", "resource ID"),
		"labels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}, []string{"id"}), func(context.Context, string) (string, error) {
		return "ok", nil
	})

	tests := []struct {
		name string
		args string
		want string
	}{
		{name: "unknown field", args: `{"id":1,"user_id":99}`, want: `unknown field "user_id"`},
		{name: "wrong integer type", args: `{"id":"1"}`, want: "must be an integer"},
		{name: "wrong array item", args: `{"id":1,"labels":["ok",2]}`, want: "must be a string"},
		{name: "missing required", args: `{}`, want: `field "id" is required`},
		{name: "trailing JSON", args: `{"id":1} {"id":2}`, want: "trailing JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := registry.ExecuteTool(context.Background(), "inspect", test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestRegistryObserverReceivesSuccessAndFailure(t *testing.T) {
	registry := NewRegistry()
	registry.Register("inspect", "read", JSONSchemaObject(map[string]any{
		"id": JSONSchemaProperty("integer", "resource ID"),
	}, []string{"id"}), func(context.Context, string) (string, error) {
		return `{"ok":true}`, nil
	})
	var events []ToolRunEvent
	registry.SetObserver(func(event ToolRunEvent) {
		events = append(events, event)
	})
	ctx := WithToolExecutionMeta(context.Background(), ToolExecutionMeta{RequestID: "req-1", UserID: 7})

	if _, err := registry.ExecuteTool(ctx, "inspect", `{"id":"bad"}`); err == nil {
		t.Fatal("expected invalid arguments to fail")
	}
	if _, err := registry.ExecuteTool(ctx, "inspect", `{"id":1}`); err != nil {
		t.Fatalf("expected successful execution, got %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected two observed runs, got %d", len(events))
	}
	if events[0].Error == "" || events[1].Error != "" {
		t.Fatalf("unexpected observer errors: first=%q second=%q", events[0].Error, events[1].Error)
	}
	if events[1].Meta.RequestID != "req-1" || events[1].Meta.UserID != 7 {
		t.Fatalf("execution metadata missing from observer: %+v", events[1].Meta)
	}
}

func TestRegistryEnforcesPerRequestCallLimit(t *testing.T) {
	registry := NewRegistry()
	registry.Register("inspect", "read", JSONSchemaObject(map[string]any{}, nil), func(context.Context, string) (string, error) {
		return "ok", nil
	})
	ctx := WithToolExecutionMeta(context.Background(), ToolExecutionMeta{RequestID: "limited-request"})
	for i := 0; i < defaultToolCallLimit; i++ {
		if _, err := registry.ExecuteTool(ctx, "inspect", "{}"); err != nil {
			t.Fatalf("call %d unexpectedly failed: %v", i+1, err)
		}
	}
	if _, err := registry.ExecuteTool(ctx, "inspect", "{}"); !errors.Is(err, ErrToolCallLimit) {
		t.Fatalf("expected call limit error, got %v", err)
	}
}
