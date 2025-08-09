package rtsp

// RTSPConfig RTSP 서버 설정
type RTSPConfig struct {
	Port    int // RTSP 서버 포트
	Timeout int // 세션 타임아웃 (초)
}

// NewRTSPConfig RTSP 설정 생성자
func NewRTSPConfig(port, timeout int) RTSPConfig {
	return RTSPConfig{
		Port:    port,
		Timeout: timeout,
	}
}

// DefaultRTSPConfig 기본 RTSP 설정 반환
func DefaultRTSPConfig() RTSPConfig {
	return RTSPConfig{
		Port:    554, // RTSP 표준 포트
		Timeout: 60,  // 60초 기본 타임아웃
	}
}
