package srt

import (
	"fmt"
	"time"
)

// ValidateConfig SRT 설정 검증
func (c *SRTConfig) ValidateConfig() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid SRT port: %d (must be 1-65535)", c.Port)
	}
	
	if c.Latency < MinLatency || c.Latency > MaxLatency {
		return fmt.Errorf("invalid SRT latency: %d ms (must be %d-%d ms)", c.Latency, MinLatency, MaxLatency)
	}
	
	if c.Encryption < EncryptionNone || c.Encryption > EncryptionAES256 {
		return fmt.Errorf("invalid encryption type: %d", c.Encryption)
	}
	
	if c.Encryption != EncryptionNone && c.Passphrase == "" {
		return fmt.Errorf("passphrase required for encryption")
	}
	
	if c.MaxBandwidth < 0 {
		return fmt.Errorf("max bandwidth cannot be negative")
	}
	
	if c.Timeout <= 0 {
		c.Timeout = 5 * time.Second
	}
	
	return nil
}

// GetEncryptionString 암호화 타입 문자열 반환
func (c *SRTConfig) GetEncryptionString() string {
	switch c.Encryption {
	case EncryptionNone:
		return "none"
	case EncryptionAES128:
		return "aes128"
	case EncryptionAES192:
		return "aes192"
	case EncryptionAES256:
		return "aes256"
	default:
		return "unknown"
	}
}

// HasEncryption 암호화 사용 여부
func (c *SRTConfig) HasEncryption() bool {
	return c.Encryption != EncryptionNone
}

// GetLatencyDuration 지연시간을 Duration으로 반환
func (c *SRTConfig) GetLatencyDuration() time.Duration {
	return time.Duration(c.Latency) * time.Millisecond
}

// SetEncryptionFromString 문자열로 암호화 타입 설정
func (c *SRTConfig) SetEncryptionFromString(encType string) error {
	switch encType {
	case "none", "":
		c.Encryption = EncryptionNone
	case "aes128":
		c.Encryption = EncryptionAES128
	case "aes192":
		c.Encryption = EncryptionAES192  
	case "aes256":
		c.Encryption = EncryptionAES256
	default:
		return fmt.Errorf("unsupported encryption type: %s", encType)
	}
	return nil
}

// Clone 설정 복사본 생성
func (c *SRTConfig) Clone() SRTConfig {
	return SRTConfig{
		Port:         c.Port,
		Latency:      c.Latency,
		MaxBandwidth: c.MaxBandwidth,
		Encryption:   c.Encryption,
		Passphrase:   c.Passphrase,
		StreamID:     c.StreamID,
		Timeout:      c.Timeout,
	}
}