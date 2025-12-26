package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug or release
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

var AppConfig *Config

func LoadConfig(configPath string) error {
	v := viper.New()

	// 默认值
	v.SetDefault("server.port", 8306)
	v.SetDefault("server.mode", "release")
	v.SetDefault("database.path", "data/animate.db")
	v.SetDefault("log.level", "info")

	// 配置文件路径
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	if configPath != "" {
		v.AddConfigPath(configPath)
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
		// Config file not found is okay, use defaults
		fmt.Println("Config file not found, using defaults")
	}

	AppConfig = &Config{}
	if err := v.Unmarshal(AppConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return nil
}
