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
	RTSP    RTSPConfig    `yaml:"rtsp"`
	API     APIConfig     `yaml:"api"`
	Logging LoggingConfig `yaml:"logging"`
	Stream  StreamConfig  `yaml:"stream"`
}

type RTMPConfig struct {
	Port int `yaml:"port"`
}

type RTSPConfig struct {
	Port    int `yaml:"port"`
	Timeout int `yaml:"timeout"`
}

type APIConfig struct {
	Port int `yaml:"port"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type StreamConfig struct {
	GopCacheSize        int `yaml:"gop_cache_size"`
	MaxPlayersPerStream int `yaml:"max_players_per_stream"`
}

// GetConfigWithDefaults returns default configuration values
func GetConfigWithDefaults() *Config {
	return &Config{
		RTMP: RTMPConfig{
			Port: 1935,
		},
		RTSP: RTSPConfig{
			Port: 554,
			Timeout: 60,
		},
		API: APIConfig{
			Port: 8080,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		Stream: StreamConfig{
			GopCacheSize:        10,
			MaxPlayersPerStream: 100,
		},
	}
}

// LoadConfig loads configuration from yaml file
func LoadConfig() (*Config, error) {
	// 기본 설정값으로 초기화
	config := GetConfigWithDefaults()

	// 설정 파일 경로 결정 (프로젝트 루트의 configs/default.yaml)
	configPath := filepath.Join("configs", "default.yaml")
	
	// 파일 존재 확인 - 없으면 기본값 사용
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("Config file not found (%s), using default values:\n", configPath)
		fmt.Printf("  RTMP Port: %d\n", config.RTMP.Port)
		fmt.Printf("  RTSP Port: %d\n", config.RTSP.Port)
		fmt.Printf("  RTSP Timeout: %d\n", config.RTSP.Timeout)
		fmt.Printf("  API Port: %d\n", config.API.Port)
		fmt.Printf("  Log Level: %s\n", config.Logging.Level)
	fmt.Printf("  GOP Cache Size: %d\n", config.Stream.GopCacheSize)
	fmt.Printf("  Max Players Per Stream: %d\n", config.Stream.MaxPlayersPerStream)
		return config, nil
	}
	
	// 파일 읽기
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	// YAML 파싱 - 기존 기본값 위에 덮어쓰기
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	// 설정 검증
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	
	fmt.Printf("Config loaded from %s:\n", configPath)
	fmt.Printf("  RTMP Port: %d\n", config.RTMP.Port)
	fmt.Printf("  RTSP Port: %d\n", config.RTSP.Port)
	fmt.Printf("  RTSP Timeout: %d\n", config.RTSP.Timeout)
	fmt.Printf("  API Port: %d\n", config.API.Port)
	fmt.Printf("  Log Level: %s\n", config.Logging.Level)
	fmt.Printf("  GOP Cache Size: %d\n", config.Stream.GopCacheSize)
	fmt.Printf("  Max Players Per Stream: %d\n", config.Stream.MaxPlayersPerStream)
	return config, nil
}

// validate checks if the configuration is valid
func (c *Config) validate() error {
	// RTMP 포트 검증
	if c.RTMP.Port <= 0 || c.RTMP.Port > 65535 {
		return fmt.Errorf("invalid rtmp port: %d (must be between 1-65535)", c.RTMP.Port)
	}
	
	// RTSP 포트 검증
	if c.RTSP.Port <= 0 || c.RTSP.Port > 65535 {
		return fmt.Errorf("invalid rtsp port: %d (must be between 1-65535)", c.RTSP.Port)
	}
	
	// RTSP 타임아웃 검증
	if c.RTSP.Timeout <= 0 {
		return fmt.Errorf("invalid rtsp timeout: %d (must be positive)", c.RTSP.Timeout)
	}
	
	// API 포트 검증
	if c.API.Port <= 0 || c.API.Port > 65535 {
		return fmt.Errorf("invalid api port: %d (must be between 1-65535)", c.API.Port)
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
