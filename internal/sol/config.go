package sol

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sol/pkg/hls"
	"sol/pkg/srt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RTMP    RTMPConfig    `yaml:"rtmp"`
	RTSP    RTSPConfig    `yaml:"rtsp"`
	SRT     SRTConfig     `yaml:"srt"`
	HLS     HLSConfig     `yaml:"hls"`
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

type SRTConfig struct {
	Port         int    `yaml:"port"`
	Latency      int    `yaml:"latency"`
	MaxBandwidth int64  `yaml:"max_bandwidth"`
	Encryption   string `yaml:"encryption"`
	Passphrase   string `yaml:"passphrase"`
	StreamID     string `yaml:"stream_id"`
	Timeout      int    `yaml:"timeout"`
}

type HLSConfig struct {
	Port            int           `yaml:"port"`
	SegmentDuration time.Duration `yaml:"segment_duration"`
	PlaylistSize    int           `yaml:"playlist_size"`
	Format          string        `yaml:"format"` // "ts" or "fmp4"
	CORSEnabled     bool          `yaml:"cors_enabled"`
	LowLatency      bool          `yaml:"low_latency"`
	StoragePath     string        `yaml:"storage_path"`
	MaxConcurrent   int           `yaml:"max_concurrent"`
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
		SRT: SRTConfig{
			Port:         9999,
			Latency:      120,
			MaxBandwidth: 0,
			Encryption:   "none",
			Passphrase:   "",
			StreamID:     "",
			Timeout:      5,
		},
		HLS: HLSConfig{
			Port:            8081,
			SegmentDuration: 6 * time.Second,
			PlaylistSize:    5,
			Format:          "ts",
			CORSEnabled:     true,
			LowLatency:      false,
			StoragePath:     "/tmp/hls",
			MaxConcurrent:   10,
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
		fmt.Printf("  SRT Port: %d\n", config.SRT.Port)
		fmt.Printf("  SRT Latency: %d ms\n", config.SRT.Latency)
		fmt.Printf("  SRT Encryption: %s\n", config.SRT.Encryption)
		fmt.Printf("  HLS Port: %d\n", config.HLS.Port)
		fmt.Printf("  HLS Segment Duration: %v\n", config.HLS.SegmentDuration)
		fmt.Printf("  HLS Format: %s\n", config.HLS.Format)
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
	fmt.Printf("  SRT Port: %d\n", config.SRT.Port)
	fmt.Printf("  SRT Latency: %d ms\n", config.SRT.Latency)
	fmt.Printf("  SRT Encryption: %s\n", config.SRT.Encryption)
	fmt.Printf("  HLS Port: %d\n", config.HLS.Port)
	fmt.Printf("  HLS Segment Duration: %v\n", config.HLS.SegmentDuration)
	fmt.Printf("  HLS Format: %s\n", config.HLS.Format)
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
	
	// SRT 포트 검증
	if c.SRT.Port <= 0 || c.SRT.Port > 65535 {
		return fmt.Errorf("invalid srt port: %d (must be between 1-65535)", c.SRT.Port)
	}
	
	// SRT 지연시간 검증
	if c.SRT.Latency < 20 || c.SRT.Latency > 8000 {
		return fmt.Errorf("invalid srt latency: %d ms (must be between 20-8000 ms)", c.SRT.Latency)
	}
	
	// SRT 암호화 타입 검증
	validEncryptions := []string{"none", "aes128", "aes192", "aes256"}
	encValid := false
	for _, enc := range validEncryptions {
		if strings.ToLower(c.SRT.Encryption) == enc {
			encValid = true
			break
		}
	}
	if !encValid {
		return fmt.Errorf("invalid srt encryption: %s (must be one of: %v)", c.SRT.Encryption, validEncryptions)
	}
	
	// SRT 암호화 사용 시 패스워드 검증
	if c.SRT.Encryption != "none" && c.SRT.Passphrase == "" {
		return fmt.Errorf("srt passphrase required when encryption is enabled")
	}
	
	// SRT 타임아웃 검증
	if c.SRT.Timeout <= 0 {
		return fmt.Errorf("invalid srt timeout: %d (must be positive)", c.SRT.Timeout)
	}
	
	// HLS 포트 검증
	if c.HLS.Port <= 0 || c.HLS.Port > 65535 {
		return fmt.Errorf("invalid hls port: %d (must be between 1-65535)", c.HLS.Port)
	}
	
	// HLS 세그먼트 길이 검증
	if c.HLS.SegmentDuration < time.Second || c.HLS.SegmentDuration > 30*time.Second {
		return fmt.Errorf("invalid hls segment duration: %v (must be between 1s-30s)", c.HLS.SegmentDuration)
	}
	
	// HLS 플레이리스트 크기 검증
	if c.HLS.PlaylistSize < 3 || c.HLS.PlaylistSize > 20 {
		return fmt.Errorf("invalid hls playlist size: %d (must be between 3-20)", c.HLS.PlaylistSize)
	}
	
	// HLS 포맷 검증
	if c.HLS.Format != "ts" && c.HLS.Format != "fmp4" {
		return fmt.Errorf("invalid hls format: %s (must be 'ts' or 'fmp4')", c.HLS.Format)
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

// ToHLSConfig converts Config.HLS to hls.HLSConfig
func (c *Config) ToHLSConfig() hls.HLSConfig {
	var format hls.SegmentFormat
	if c.HLS.Format == "fmp4" {
		format = hls.SegmentFormatFMP4
	} else {
		format = hls.SegmentFormatTS
	}
	
	return hls.HLSConfig{
		Port:            c.HLS.Port,
		SegmentDuration: c.HLS.SegmentDuration,
		PlaylistSize:    c.HLS.PlaylistSize,
		Format:          format,
		CORSEnabled:     c.HLS.CORSEnabled,
		LowLatency:      c.HLS.LowLatency,
		StoragePath:     c.HLS.StoragePath,
		MaxConcurrent:   c.HLS.MaxConcurrent,
	}
}

// ToSRTConfig converts Config.SRT to srt.SRTConfig
func (c *Config) ToSRTConfig() srt.SRTConfig {
	var encryption int
	switch strings.ToLower(c.SRT.Encryption) {
	case "aes128":
		encryption = srt.EncryptionAES128
	case "aes192":
		encryption = srt.EncryptionAES192
	case "aes256":
		encryption = srt.EncryptionAES256
	default:
		encryption = srt.EncryptionNone
	}
	
	return srt.SRTConfig{
		Port:         c.SRT.Port,
		Latency:      c.SRT.Latency,
		MaxBandwidth: c.SRT.MaxBandwidth,
		Encryption:   encryption,
		Passphrase:   c.SRT.Passphrase,
		StreamID:     c.SRT.StreamID,
		Timeout:      time.Duration(c.SRT.Timeout) * time.Second,
	}
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
