package core

// Server 프로토콜별 서버들의 공통 인터페이스
// RTMP, RTSP 등 모든 프로토콜 서버가 구현해야 하는 기본 메서드들을 정의
type Server interface {
	Start() error
	Stop()
	ID() string
}
