package qbutil

import (
	"log"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/launcher"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

const (
	DefaultURL   = "http://localhost:8080"
	LegacyQBURL  = "http://localhost:7603"
	ModeManaged  = "managed"
	ModeExternal = "external"
)

type Config struct {
	URL      string
	Username string
	Password string
	Mode     string
}

func LoadConfig() Config {
	cfg := Config{
		Mode: ModeManaged,
	}

	if db.DB == nil {
		applyManagedBootstrapCredentials(&cfg)
		return cfg
	}

	rawURL := ""
	var configs []model.GlobalConfig
	if err := db.DB.Find(&configs).Error; err != nil {
		log.Printf("Error fetching QB config: %v", err)
		cfg.URL = DefaultURL
		return cfg
	}

	for _, item := range configs {
		switch item.Key {
		case model.ConfigKeyQBUrl:
			rawURL = strings.TrimSpace(item.Value)
		case model.ConfigKeyQBUsername:
			cfg.Username = strings.TrimSpace(item.Value)
		case model.ConfigKeyQBPassword:
			cfg.Password = strings.TrimSpace(item.Value)
		case model.ConfigKeyQBMode:
			cfg.Mode = NormalizeMode(item.Value)
		}
	}

	cfg.URL = rawURL
	if cfg.Mode == "" {
		cfg.Mode = deriveModeFromURL(cfg.URL)
	}

	if UsesManagedInstance(cfg) {
		cfg.Mode = ModeManaged
		applyManagedBootstrapCredentials(&cfg)
	} else {
		cfg.Mode = ModeExternal
		cfg.URL = strings.TrimSpace(cfg.URL)
	}

	return cfg
}

func NormalizeURL(raw string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(raw)), "/")
}

func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeExternal:
		return ModeExternal
	default:
		return ModeManaged
	}
}

func IsManagedLocalURL(raw string) bool {
	switch NormalizeURL(raw) {
	case NormalizeURL(DefaultURL),
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
		NormalizeURL(LegacyQBURL),
		"http://127.0.0.1:7603",
		"http://[::1]:7603":
		return true
	default:
		return false
	}
}

func UsesManagedInstance(cfg Config) bool {
	if NormalizeMode(cfg.Mode) == ModeExternal {
		return false
	}

	return cfg.URL == "" || IsManagedLocalURL(cfg.URL)
}

func MissingExternalURL(cfg Config) bool {
	return NormalizeMode(cfg.Mode) == ModeExternal && strings.TrimSpace(cfg.URL) == ""
}

func ManagedBinaryMissing(cfg Config, binDir string) bool {
	if !UsesManagedInstance(cfg) {
		return false
	}

	return !launcher.HasManagedQBBinary(binDir)
}

func deriveModeFromURL(raw string) string {
	if raw == "" || IsManagedLocalURL(raw) {
		return ModeManaged
	}
	return ModeExternal
}

func applyManagedBootstrapCredentials(cfg *Config) {
	cfg.URL = DefaultURL
	cfg.Username = ""
	cfg.Password = ""

	creds, err := bootstrap.LoadQBCredentials()
	if err != nil {
		return
	}

	if strings.TrimSpace(creds.URL) != "" {
		cfg.URL = strings.TrimSpace(creds.URL)
	}
	cfg.Username = strings.TrimSpace(creds.Username)
	cfg.Password = strings.TrimSpace(creds.Password)
}
