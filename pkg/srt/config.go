package srt

// SRTConfig SRT 서버 설정
type SRTConfig struct {
	Port         int  // SRT 포트 (기본: 9998)
	Timeout      int  // 연결 타임아웃 (초)
	Latency      int  // SRT 지연시간 (ms, 기본: 120)
	MaxBandwidth int  // 최대 대역폭 (bytes/sec, 기본: -1은 무제한)
	PeerTimeout  int  // 피어 타임아웃 (ms, 기본: 5000)
	EnableTLS    bool // TLS 암호화 사용 여부
}

// NewSRTConfig 기본 SRT 설정 생성
func NewSRTConfig(port, timeout int) SRTConfig {
	return SRTConfig{
		Port:         port,
		Timeout:      timeout,
		Latency:      120,   // 기본 120ms
		MaxBandwidth: -1,    // 무제한
		PeerTimeout:  5000,  // 5초
		EnableTLS:    false, // TLS 비활성화
	}
}

// DefaultSRTConfig 기본 SRT 설정 반환
func DefaultSRTConfig() SRTConfig {
	return NewSRTConfig(9998, 30)
}