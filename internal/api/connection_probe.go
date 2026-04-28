package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type connectionProbe struct {
	cacheKey   string
	configHash string
}

func newConnectionProbe(cacheKey string, hashParts ...string) connectionProbe {
	return connectionProbe{
		cacheKey:   cacheKey,
		configHash: getCacheHash(hashParts...),
	}
}

func (p connectionProbe) load() (cachedStatus, bool) {
	val, ok := statusCache.Load(p.cacheKey)
	if !ok {
		return cachedStatus{}, false
	}
	stat := val.(cachedStatus)
	if stat.ConfigHash != p.configHash || time.Now().After(stat.Expiry) {
		return cachedStatus{}, false
	}
	return stat, true
}

func (p connectionProbe) store(success bool, msg, msg2 string, ttl time.Duration) {
	statusCache.Store(p.cacheKey, cachedStatus{
		Success:    success,
		Msg:        msg,
		Msg2:       msg2,
		ConfigHash: p.configHash,
		Expiry:     time.Now().Add(ttl),
	})
}

func loadProxySettings(flagKey string) (string, string) {
	var proxyEnabled model.GlobalConfig
	var proxyConfig model.GlobalConfig
	db.DB.Where("key = ?", flagKey).First(&proxyEnabled)
	if proxyEnabled.Value == ValueTrue {
		db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyConfig)
	}
	return proxyEnabled.Value, proxyConfig.Value
}

func buildProxyTransport(enabledValue, proxyURL string) *http.Transport {
	if enabledValue != ValueTrue || proxyURL == "" {
		return nil
	}
	proxyURL = proxyURL
	if parsed, err := url.Parse(proxyURL); err == nil {
		return &http.Transport{Proxy: http.ProxyURL(parsed)}
	}
	return nil
}
