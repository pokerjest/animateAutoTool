package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
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
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("API error (status %d, code %s)", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("API error (status %d)", e.StatusCode)
}

func ProviderStatusCode(err error) int {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode
	}
	return 0
}

func ProviderErrorCode(err error) string {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Code
	}
	return ""
}

func ProviderRetryAfter(err error) time.Duration {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.RetryAfter
	}
	return 0
}

const maxProviderErrorBody = 64 << 10

func NewProviderErrorFromResponse(resp *http.Response) *ProviderError {
	if resp == nil {
		return &ProviderError{}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxProviderErrorBody))
	providerErr := &ProviderError{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		RetryAfter: parseProviderRetryAfter(resp.Header.Get("Retry-After")),
	}
	parseProviderErrorBody(providerErr, body)
	return providerErr
}

func parseProviderErrorBody(providerErr *ProviderError, body []byte) {
	if providerErr == nil || len(body) == 0 {
		return
	}
	var payload struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Error) == 0 {
		return
	}

	var text string
	if json.Unmarshal(payload.Error, &text) == nil {
		providerErr.Message = strings.TrimSpace(text)
		return
	}

	var detail struct {
		Code    json.RawMessage   `json:"code"`
		Message string            `json:"message"`
		Status  string            `json:"status"`
		Type    string            `json:"type"`
		Details []json.RawMessage `json:"details"`
	}
	if json.Unmarshal(payload.Error, &detail) != nil {
		return
	}
	providerErr.Message = strings.TrimSpace(detail.Message)
	providerErr.Code = firstProviderErrorCode(detail.Status, detail.Type, detail.Code)
	for _, rawDetail := range detail.Details {
		var item map[string]any
		if json.Unmarshal(rawDetail, &item) != nil {
			continue
		}
		if retryAfter := findProviderRetryDelay(item); retryAfter > 0 {
			providerErr.RetryAfter = retryAfter
			break
		}
	}
}

func firstProviderErrorCode(status, errorType string, rawCode json.RawMessage) string {
	for _, value := range []string{status, errorType} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	var text string
	if json.Unmarshal(rawCode, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(rawCode, &number) == nil {
		return number.String()
	}
	return ""
}

func findProviderRetryDelay(value any) time.Duration {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			switch strings.ToLower(key) {
			case "retrydelay", "retry_delay":
				if duration := parseProviderRetryAfter(fmt.Sprint(child)); duration > 0 {
					return duration
				}
			}
			if duration := findProviderRetryDelay(child); duration > 0 {
				return duration
			}
		}
	case []any:
		for _, child := range current {
			if duration := findProviderRetryDelay(child); duration > 0 {
				return duration
			}
		}
	}
	return 0
}

func parseProviderRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		if duration := time.Until(retryAt); duration > 0 {
			return duration
		}
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
