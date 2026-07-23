package service

import (
	"log"

	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func configuredProxyURL(flagKey string) string {
	if flagKey != "" && configValue(flagKey) != model.ConfigValueTrue {
		return ""
	}
	normalized, err := httpx.NormalizeProxyURL(configValue(model.ConfigKeyProxyURL))
	if err != nil {
		log.Printf("Ignoring invalid configured proxy URL for %s: %v", flagKey, err)
		return ""
	}
	return normalized
}
