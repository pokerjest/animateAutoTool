package api

import (
	"fmt"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type aiProviderSettings struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Format   string `json:"format"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"-"`
	Model    string `json:"model"`
	HasKey   bool   `json:"has_key"`
}

type aiProviderKeySet struct {
	BaseURL string
	APIKey  string
	Model   string
	Format  string
}

var aiProviderKeys = map[string]aiProviderKeySet{
	ai.ProviderOpenAI: {
		BaseURL: model.ConfigKeyAIOpenAIBaseURL,
		APIKey:  model.ConfigKeyAIOpenAIAPIKey,
		Model:   model.ConfigKeyAIOpenAIModel,
	},
	ai.ProviderGemini: {
		BaseURL: model.ConfigKeyAIGeminiBaseURL,
		APIKey:  model.ConfigKeyAIGeminiAPIKey,
		Model:   model.ConfigKeyAIGeminiModel,
		Format:  model.ConfigKeyAIGeminiFormat,
	},
	ai.ProviderClaude: {
		BaseURL: model.ConfigKeyAIClaudeBaseURL,
		APIKey:  model.ConfigKeyAIClaudeAPIKey,
		Model:   model.ConfigKeyAIClaudeModel,
		Format:  model.ConfigKeyAIClaudeFormat,
	},
}

func aiProviderLabel(provider string) string {
	switch provider {
	case ai.ProviderGemini:
		return "Google Gemini"
	case ai.ProviderClaude:
		return "Anthropic Claude"
	default:
		return "OpenAI / GPT"
	}
}

func defaultAIBaseURL(provider, format string) string {
	if format == ai.ProviderFormatOpenAI {
		switch provider {
		case ai.ProviderGemini:
			return "https://generativelanguage.googleapis.com/v1beta/openai"
		case ai.ProviderClaude:
			return "https://api.anthropic.com/v1"
		default:
			return "https://api.openai.com/v1"
		}
	}
	switch provider {
	case ai.ProviderGemini:
		return "https://generativelanguage.googleapis.com"
	case ai.ProviderClaude:
		return "https://api.anthropic.com"
	default:
		return "https://api.openai.com/v1"
	}
}

func isDefaultAIBaseURL(provider, baseURL string) bool {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if normalized == "" {
		return true
	}
	for _, format := range []string{ai.ProviderFormatNative, ai.ProviderFormatOpenAI} {
		if normalized == defaultAIBaseURL(provider, format) {
			return true
		}
	}
	return false
}

func configuredAIProvider() string {
	provider, err := ai.NormalizeProvider(configValue(model.ConfigKeyAIProvider))
	if err != nil {
		return ai.ProviderOpenAI
	}
	return provider
}

func loadAIProviderSettings(provider string) aiProviderSettings {
	normalized, err := ai.NormalizeProvider(provider)
	if err != nil {
		normalized = ai.ProviderOpenAI
	}
	keys := aiProviderKeys[normalized]
	baseURL := strings.TrimSpace(configValue(keys.BaseURL))
	apiKey := strings.TrimSpace(configValue(keys.APIKey))
	modelName := strings.TrimSpace(configValue(keys.Model))
	format, formatErr := ai.NormalizeProviderFormat(normalized, configValue(keys.Format))
	if formatErr != nil {
		format, _ = ai.NormalizeProviderFormat(normalized, "")
	}
	if normalized == ai.ProviderOpenAI {
		if baseURL == "" {
			baseURL = strings.TrimSpace(configValue(model.ConfigKeyAIBaseURL))
		}
		if apiKey == "" {
			apiKey = strings.TrimSpace(configValue(model.ConfigKeyAIApiKey))
		}
		if modelName == "" {
			modelName = strings.TrimSpace(configValue(model.ConfigKeyAIModel))
		}
	}
	if baseURL == "" {
		baseURL = defaultAIBaseURL(normalized, format)
	}
	if normalized == ai.ProviderOpenAI && modelName == "" {
		modelName = defaultAIModel
	}
	return aiProviderSettings{
		Provider: normalized,
		Label:    aiProviderLabel(normalized),
		Format:   format,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Model:    modelName,
		HasKey:   apiKey != "",
	}
}

func activeAIProviderSettings() aiProviderSettings {
	return loadAIProviderSettings(configuredAIProvider())
}

func buildAIClient(settings aiProviderSettings) (ai.CompletionClient, error) {
	return ai.NewProviderClient(ai.ProviderConfig{
		Provider: settings.Provider,
		Format:   settings.Format,
		BaseURL:  settings.BaseURL,
		APIKey:   settings.APIKey,
		Model:    settings.Model,
		ProxyURL: configuredProxyURL(model.ConfigKeyProxyAI),
	})
}

func modelListAIProviderSettings(settings aiProviderSettings) aiProviderSettings {
	if settings.Provider == ai.ProviderClaude &&
		settings.Format == ai.ProviderFormatOpenAI &&
		strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/") == defaultAIBaseURL(ai.ProviderClaude, ai.ProviderFormatOpenAI) {
		settings.Format = ai.ProviderFormatNative
		settings.BaseURL = defaultAIBaseURL(ai.ProviderClaude, ai.ProviderFormatNative)
	}
	return settings
}

func buildAIModelClient(settings aiProviderSettings) (ai.CompletionClient, error) {
	return buildAIClient(modelListAIProviderSettings(settings))
}

type aiProviderInput struct {
	Provider string `json:"provider"`
	Format   string `json:"format"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

func resolveAIProviderInput(input aiProviderInput) (aiProviderSettings, error) {
	return resolveAIProviderInputWithRequirements(input, true)
}

func resolveAIProviderModelListInput(input aiProviderInput) (aiProviderSettings, error) {
	return resolveAIProviderInputWithRequirements(input, false)
}

func resolveAIProviderInputWithRequirements(input aiProviderInput, requireModel bool) (aiProviderSettings, error) {
	provider, err := ai.NormalizeProvider(input.Provider)
	if err != nil {
		return aiProviderSettings{}, err
	}
	saved := loadAIProviderSettings(provider)
	format, err := ai.NormalizeProviderFormat(provider, input.Format)
	if err != nil {
		return aiProviderSettings{}, err
	}
	if strings.TrimSpace(input.Format) == "" {
		format = saved.Format
	}
	formatChanged := format != saved.Format
	saved.Format = format
	if value := strings.TrimSpace(input.BaseURL); value != "" {
		saved.BaseURL = strings.TrimRight(value, "/")
	} else if formatChanged && isDefaultAIBaseURL(provider, saved.BaseURL) {
		saved.BaseURL = defaultAIBaseURL(provider, format)
	}
	if value := strings.TrimSpace(input.APIKey); value != "" {
		saved.APIKey = value
		saved.HasKey = true
	}
	if value := strings.TrimSpace(input.Model); value != "" {
		saved.Model = value
	}
	if strings.TrimSpace(saved.APIKey) == "" {
		return aiProviderSettings{}, fmt.Errorf("%s API Key 未配置", saved.Label)
	}
	if requireModel && strings.TrimSpace(saved.Model) == "" {
		return aiProviderSettings{}, fmt.Errorf("%s 模型未配置", saved.Label)
	}
	return saved, nil
}
