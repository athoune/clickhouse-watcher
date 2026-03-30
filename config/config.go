package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Connections map[string]ConnectionConfig `mapstructure:"connections"`
	Log         LogConfig                   `mapstructure:"log"`
}

type ConnectionConfig struct {
	Name     string `mapstructure:"name"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Database string `mapstructure:"database"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Path   string `mapstructure:"path"`
	Pretty bool   `mapstructure:"pretty"`
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home dir: %w", err)
	}

	// Set defaults
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.path", "") // Empty means stderr
	viper.SetDefault("log.pretty", false)

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath(home + "/.config/clickhouse-watcher")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) GetFirstConnection() *ConnectionConfig {
	for _, conn := range c.Connections {
		return &conn
	}
	return nil
}

// GetLogConfig returns logging configuration with defaults applied
func (c *Config) GetLogConfig() LogConfig {
	cfg := c.Log

	// Apply defaults if not set
	if cfg.Level == "" {
		cfg.Level = "info"
	}

	return cfg
}
