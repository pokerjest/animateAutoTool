package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/security"
	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Log      LogConfig      `mapstructure:"log"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Managed  ManagedConfig  `mapstructure:"managed_services"`
}

type Paths struct {
	RootDir    string
	ConfigFile string
	DataDir    string
	LogsDir    string
	BinDir     string
}

type AuthConfig struct {
	SecretKey string `mapstructure:"secret_key"`
}

type ServerConfig struct {
	Port           int      `mapstructure:"port"`
	Mode           string   `mapstructure:"mode"` // debug or release
	PublicURL      string   `mapstructure:"public_url"`
	Headless       bool     `mapstructure:"headless"`
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

type ManagedConfig struct {
	DownloadMissing bool `mapstructure:"download_missing"`
}

var AppConfig *Config
var AppPaths Paths
var ConfigAutoCreated bool

const (
	appName               = "AnimateAutoTool"
	defaultAuthSecret     = "change_me_random_string"
	defaultConfigFileName = "config.yaml"
)

var appRootOverride string
var authSecretFallbackPath string
var authSecretFallbackPathOverride string

func LoadConfig(configPath string) error {
	AppPaths = resolveAppPaths(configPath)
	authSecretFallbackPath = resolvedAuthSecretFallbackPath()
	ConfigAutoCreated = false

	v := viper.New()

	// 默认值
	v.SetDefault("server.port", 8306)
	v.SetDefault("server.mode", "release")
	v.SetDefault("server.public_url", "")
	v.SetDefault("server.headless", runtime.GOOS != "windows")
	v.SetDefault("server.trusted_proxies", []string{"127.0.0.1", "::1"})
	v.SetDefault("database.path", filepath.Join(AppPaths.DataDir, "animate.db"))
	v.SetDefault("log.level", "info")
	v.SetDefault("alist_url", "http://alist:5244") // Docker internal default
	v.SetDefault("auth.secret_key", defaultAuthSecret)
	v.SetDefault("managed_services.download_missing", false)

	// 配置文件路径
	v.SetConfigName(strings.TrimSuffix(defaultConfigFileName, filepath.Ext(defaultConfigFileName)))
	v.SetConfigType("yaml")
	v.AddConfigPath(AppPaths.RootDir)

	if !hasConfigFile(AppPaths.RootDir) {
		if err := writeDefaultConfigFile(); err != nil {
			return fmt.Errorf("failed to initialize default config file: %w", err)
		}
		ConfigAutoCreated = true
		fmt.Printf("Config file not found. Created a default config at %s\n", AppPaths.ConfigFile)
	}

	// 环境变量替换 (使用 ANIME_ 前缀)
	// 比如 ANIME_SERVER_PORT=9090
	v.SetEnvPrefix("ANIME")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		fmt.Println("Config file not found, using defaults")
	}

	AppConfig = &Config{}
	if err := v.Unmarshal(AppConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if AppConfig.Database.Path == "" {
		AppConfig.Database.Path = filepath.Join(AppPaths.DataDir, "animate.db")
	} else if !filepath.IsAbs(AppConfig.Database.Path) {
		AppConfig.Database.Path = filepath.Join(AppPaths.RootDir, AppConfig.Database.Path)
	}

	if AppConfig.Auth.SecretKey == "" || AppConfig.Auth.SecretKey == defaultAuthSecret {
		secret, created, err := loadOrCreateFallbackAuthSecret()
		if err != nil {
			return fmt.Errorf("failed to resolve auth secret: %w", err)
		}
		AppConfig.Auth.SecretKey = secret
		if created {
			fmt.Printf("auth.secret_key missing or using default placeholder; generated and persisted a fallback secret at %s\n", authSecretFallbackPath)
		}
	}

	return nil
}

func resolveAppPaths(configPath string) Paths {
	if dir := explicitConfigDir(configPath); dir != "" {
		return newPaths(dir)
	}

	defaultRoot := defaultAppRoot()
	for _, dir := range configSearchDirs(configPath, defaultRoot) {
		if dir == "" {
			continue
		}
		if hasConfigFile(dir) {
			return newPaths(dir)
		}
	}

	return newPaths(defaultRoot)
}

func configSearchDirs(configPath, defaultRoot string) []string {
	var dirs []string
	if dir := explicitConfigDir(configPath); dir != "" {
		dirs = append(dirs, dir)
	}
	if exeDir, err := executableDir(); err == nil {
		dirs = append(dirs, exeDir)
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	dirs = append(dirs, defaultRoot)

	seen := make(map[string]struct{}, len(dirs))
	unique := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		unique = append(unique, abs)
	}

	return unique
}

func explicitConfigDir(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return ""
	}

	lower := strings.ToLower(configPath)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return filepath.Dir(configPath)
	}

	return configPath
}

func executableDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exePath), nil
}

func hasConfigFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, defaultConfigFileName))
	return err == nil
}

func newPaths(root string) Paths {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}

	return Paths{
		RootDir:    absRoot,
		ConfigFile: filepath.Join(absRoot, defaultConfigFileName),
		DataDir:    filepath.Join(absRoot, "data"),
		LogsDir:    filepath.Join(absRoot, "logs"),
		BinDir:     filepath.Join(absRoot, "bin"),
	}
}

func defaultAppRoot() string {
	if appRootOverride != "" {
		return appRootOverride
	}

	if exeDir, err := executableDir(); err == nil && strings.TrimSpace(exeDir) != "" {
		return exeDir
	}

	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, appName)
		}
	}

	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, appName)
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "."+strings.ToLower(appName))
	}

	return appName
}

func resolvedAuthSecretFallbackPath() string {
	if authSecretFallbackPathOverride != "" {
		return authSecretFallbackPathOverride
	}
	return filepath.Join(AppPaths.DataDir, "bootstrap", "auth_secret")
}

func RootDir() string {
	if AppPaths.RootDir != "" {
		return AppPaths.RootDir
	}
	return "."
}

func DataDir() string {
	if AppPaths.DataDir != "" {
		return AppPaths.DataDir
	}
	return "data"
}

func LogsDir() string {
	if AppPaths.LogsDir != "" {
		return AppPaths.LogsDir
	}
	return "logs"
}

func BinDir() string {
	if AppPaths.BinDir != "" {
		return AppPaths.BinDir
	}
	return "bin"
}

func DataPath(parts ...string) string {
	base := DataDir()
	if len(parts) == 0 {
		return base
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

func ConfigFilePath() string {
	if AppPaths.ConfigFile != "" {
		return AppPaths.ConfigFile
	}
	return defaultConfigFileName
}

func writeDefaultConfigFile() error {
	if err := os.MkdirAll(AppPaths.RootDir, 0755); err != nil {
		return err
	}

	content := strings.TrimSpace(`
# Server Configuration
server:
  port: 8306
  mode: release
  public_url: ""
  headless: false
  trusted_proxies:
    - 127.0.0.1
    - ::1

# Database Configuration
database:
  path: data/animate.db

# Logging
log:
  level: info

# Authentication
auth:
  secret_key: ""

# Managed sidecars
managed_services:
  download_missing: false
`) + "\n"

	return os.WriteFile(AppPaths.ConfigFile, []byte(content), 0644)
}

func loadOrCreateFallbackAuthSecret() (secret string, created bool, err error) {
	if data, readErr := os.ReadFile(authSecretFallbackPath); readErr == nil {
		secret = strings.TrimSpace(string(data))
		if secret != "" {
			return secret, false, nil
		}
	} else if !os.IsNotExist(readErr) {
		return "", false, readErr
	}

	secret, err = security.RandomHex(32)
	if err != nil {
		return "", false, err
	}

	if err := os.MkdirAll(filepath.Dir(authSecretFallbackPath), 0700); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(authSecretFallbackPath, []byte(secret+"\n"), 0600); err != nil {
		return "", false, err
	}

	return secret, true, nil
}
