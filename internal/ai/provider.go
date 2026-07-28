package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	ProviderOpenAI = "openai"
	ProviderGemini = "gemini"
	ProviderClaude = "claude"

	ProviderFormatNative = "native"
	ProviderFormatOpenAI = "openai"
)

type CompletionClient interface {
	CreateChatCompletion(context.Context, ChatCompletionRequest) (*ChatCompletionResponse, error)
	ListModels(context.Context) ([]string, error)
}

type ProviderConfig struct {
	Provider string
	Format   string
	BaseURL  string
	APIKey   string
	Model    string
	ProxyURL string
}

type ProviderError struct {
	StatusCode int
	Body       string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("API error (status %d)", e.StatusCode)
	}
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Body)
}

func ProviderStatusCode(err error) int {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode
	}
	return 0
}

func NormalizeProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", ProviderOpenAI:
		return ProviderOpenAI, nil
	case ProviderGemini:
		return ProviderGemini, nil
	case ProviderClaude, "anthropic":
		return ProviderClaude, nil
	default:
		return "", fmt.Errorf("unsupported AI provider %q", provider)
	}
}

func NormalizeProviderFormat(provider, format string) (string, error) {
	normalizedProvider, err := NormalizeProvider(provider)
	if err != nil {
		return "", err
	}
	normalizedFormat := strings.ToLower(strings.TrimSpace(format))
	if normalizedProvider == ProviderOpenAI {
		switch normalizedFormat {
		case "", ProviderFormatOpenAI:
			return ProviderFormatOpenAI, nil
		default:
			return "", fmt.Errorf("%s only supports the OpenAI-compatible format", normalizedProvider)
		}
	}
	switch normalizedFormat {
	case "", ProviderFormatNative:
		return ProviderFormatNative, nil
	case ProviderFormatOpenAI:
		return ProviderFormatOpenAI, nil
	default:
		return "", fmt.Errorf("unsupported %s API format %q", normalizedProvider, format)
	}
}

func NewProviderClient(cfg ProviderConfig) (CompletionClient, error) {
	provider, err := NormalizeProvider(cfg.Provider)
	if err != nil {
		return nil, err
	}
	format, err := NormalizeProviderFormat(provider, cfg.Format)
	if err != nil {
		return nil, err
	}
	if format == ProviderFormatOpenAI {
		return NewClientWithProxy(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.ProxyURL), nil
	}
	switch provider {
	case ProviderGemini:
		return NewGeminiClientWithProxy(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.ProxyURL), nil
	case ProviderClaude:
		return NewClaudeClientWithProxy(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.ProxyURL), nil
	default:
		return NewClientWithProxy(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.ProxyURL), nil
	}
}
