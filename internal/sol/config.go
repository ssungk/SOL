package sol

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RTMP    RTMPConfig    `yaml:"rtmp"`
	Logging LoggingConfig `yaml:"logging"`
	Stream  StreamConfig  `yaml:"stream"`
}

type RTMPConfig struct {
	Port int `yaml:"port"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type StreamConfig struct {
	GopCacheSize        int `yaml:"gop_cache_size"`
	MaxPlayersPerStream int `yaml:"max_players_per_stream"`
}

// LoadConfig loads configuration from yaml file
func LoadConfig() (*Config, error) {
	// 설정 파일 경로 결정 (프로젝트 루트의 configs/default.yaml)
	configPath := filepath.Join("configs", "default.yaml")
	
	// 파일 존재 확인
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}
	
	// 파일 읽기
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	// YAML 파싱
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	// 기본값 설정 및 검증
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	
	return &config, nil
}

// validate checks if the configuration is valid
func (c *Config) validate() error {
	// RTMP 포트 검증
	if c.RTMP.Port <= 0 || c.RTMP.Port > 65535 {
		return fmt.Errorf("invalid rtmp port: %d (must be between 1-65535)", c.RTMP.Port)
	}
	
	// 로그 레벨 검증
	validLevels := []string{"debug", "info", "warn", "error"}
	levelValid := false
	for _, level := range validLevels {
		if strings.ToLower(c.Logging.Level) == level {
			levelValid = true
			break
		}
	}
	if !levelValid {
		return fmt.Errorf("invalid log level: %s (must be one of: %v)", c.Logging.Level, validLevels)
	}
	
	// 스트림 설정 검증
	if c.Stream.GopCacheSize < 0 {
		return fmt.Errorf("invalid gop_cache_size: %d (must be non-negative)", c.Stream.GopCacheSize)
	}
	
	if c.Stream.MaxPlayersPerStream < 0 {
		return fmt.Errorf("invalid max_players_per_stream: %d (must be non-negative)", c.Stream.MaxPlayersPerStream)
	}
	
	return nil
}

// GetSlogLevel returns slog.Level from config
func (c *Config) GetSlogLevel() slog.Level {
	switch strings.ToLower(c.Logging.Level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // 기본값
	}
}
