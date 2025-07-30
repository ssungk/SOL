package rtmp

// 세션 종료 이벤트
type Terminated struct {
	Id string
}

// Publish 시작 이벤트
type PublishStarted struct {
	SessionId  string
	StreamName string
	StreamId   uint32
}

// Publish 종료 이벤트
type PublishStopped struct {
	SessionId  string
	StreamName string
	StreamId   uint32
}

// Play 시작 이벤트
type PlayStarted struct {
	SessionId  string
	StreamName string
	StreamId   uint32
}

// Play 종료 이벤트
type PlayStopped struct {
	SessionId  string
	StreamName string
	StreamId   uint32
}

// 오디오 데이터 수신 이벤트
type AudioData struct {
	SessionId  string
	StreamName string
	Timestamp  uint32
	Data       []byte
}

// 비디오 데이터 수신 이벤트
type VideoData struct {
	SessionId  string
	StreamName string
	Timestamp  uint32
	FrameType  string
	Data       []byte
}

// 메타데이터 수신 이벤트
type MetaData struct {
	SessionId  string
	StreamName string
	Metadata   map[string]any
}

// 새로운 연결 이벤트
type ConnectionEstablished struct {
	SessionId string
	RemoteAddr string
}

// 연결 종료 이벤트
type ConnectionClosed struct {
	SessionId string
	Reason    string
}

// 스트림 생성 이벤트
type StreamCreated struct {
	SessionId string
	StreamId  uint32
}

// 에러 이벤트
type ErrorOccurred struct {
	SessionId string
	Error     error
	Context   string
}

// FCUnpublish 이벤트
type FCUnpublishReceived struct {
	SessionId  string
	StreamName string
	StreamId   uint32
}

// 레거시 이벤트 타입들 (하위 호환성)
type Publish struct {
	Id string
}

type Play struct {
	Id string
}
