package srt

// SRTConfig SRT 서버 설정
type SRTConfig struct {
	Port    int // SRT 포트 (기본: 9998)
	Timeout int // 연결 타임아웃 (초)
}

// NewSRTConfig 기본 SRT 설정 생성
func NewSRTConfig(port, timeout int) SRTConfig {
	return SRTConfig{
		Port:    port,
		Timeout: timeout,
	}
}