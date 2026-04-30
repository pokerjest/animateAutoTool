package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(defs))
	}
	if defs[0].Type != "function" || defs[0].Function.Name != "echo" {
		t.Fatalf("unexpected definition: %+v", defs[0])
	}

	got, err := r.ExecuteTool(context.Background(), "echo", `{"text":"hi"}`)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if got != `{"text":"hi"}` {
		t.Fatalf("expected echoed args, got %q", got)
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
	r.Register("explode", "Always errors.", nil, func(ctx context.Context, args string) (string, error) {
		return "", wantErr
	})

	out, err := r.ExecuteTool(context.Background(), "explode", "{}")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped wantErr, got %v", err)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected error string passed back to LLM, got %q", out)
	}
}

func TestJSONSchemaHelpers(t *testing.T) {
	prop := JSONSchemaProperty("string", "an episode title")
	if prop["type"] != "string" || prop["description"] != "an episode title" {
		t.Fatalf("unexpected property: %+v", prop)
	}

	schema, ok := JSONSchemaObject(map[string]any{
		"title": prop,
	}, []string{"title"}).(*JSONSchemaHelper)
	if !ok {
		t.Fatalf("expected *JSONSchemaHelper, got %T", JSONSchemaObject(nil, nil))
	}
	if schema.Type != "object" {
		t.Errorf("expected type=object, got %q", schema.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "title" {
		t.Errorf("expected required=[title], got %v", schema.Required)
	}
	if _, ok := schema.Properties["title"]; !ok {
		t.Errorf("expected title property to be present")
	}
}
