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

func TestNewClientDefaultsBaseURLAndTrimsTrailingSlash(t *testing.T) {
	if got := NewClient("", "key", "model").baseURL; got != "https://api.openai.com/v1" {
		t.Errorf("default baseURL = %q", got)
	}
	if got := NewClient("https://example.com/v1/", "key", "model").baseURL; got != "https://example.com/v1" {
		t.Errorf("trim trailing slash = %q", got)
	}
}

func TestCreateChatCompletionUsesConfiguredModelAndAuth(t *testing.T) {
	var capturedAuth, capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		_, _ = w.Write([]byte(`{"id":"chat-1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret-key", "default-model")
	resp, err := c.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hi" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if capturedAuth != "Bearer secret-key" {
		t.Errorf("expected Bearer auth header, got %q", capturedAuth)
	}

	var sent ChatCompletionRequest
	if err := json.Unmarshal([]byte(capturedBody), &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if sent.Model != "default-model" {
		t.Errorf("expected fallback to client model, got %q", sent.Model)
	}
}

func TestCreateChatCompletionNonOKReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	_, err := c.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "API error") {
		t.Fatalf("expected API error, got %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected status code in error, got %v", err)
	}
}

func TestCreateChatCompletionEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"x","choices":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	_, err := c.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("expected empty choices error, got %v", err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-4o-mini" || models[1] != "gpt-4o" {
		t.Fatalf("unexpected models: %v", models)
	}
}

func TestListModelsNonOKReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`oops`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	if _, err := c.ListModels(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "API error") {
		t.Fatalf("expected API error, got %v", err)
	}
}
