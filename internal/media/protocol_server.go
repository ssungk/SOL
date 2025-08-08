package media

// ProtocolServer 프로토콜별 서버들의 공통 인터페이스
// RTMP, RTSP, HLS 등 모든 프로토콜 서버가 구현해야 하는 기본 메서드들을 정의
type ProtocolServer interface {
	// 서버 시작
	Start() error
	
	// 서버 중지
	Stop()
	
	// 서버 이름 (식별자로 사용: "rtmp", "rtsp", "hls")
	Name() string
}