package media

// NodeEvent 모든 노드 이벤트의 기본 인터페이스
type NodeEvent interface {
	GetSessionId() string
	GetStreamName() string
	EventType() string
}

// BaseNodeEvent 공통 노드 이벤트 필드
type BaseNodeEvent struct {
	SessionId  string
	StreamName string
	NodeType   MediaNodeType
}

func (e BaseNodeEvent) GetSessionId() string {
	return e.SessionId
}

func (e BaseNodeEvent) GetStreamName() string {
	return e.StreamName
}

// NodeConnected 노드 연결 이벤트
type NodeConnected struct {
	BaseNodeEvent
	NodeId  uintptr
	Address string
}

func (e NodeConnected) EventType() string {
	return "NodeConnected"
}

// NodeDisconnected 노드 연결 해제 이벤트
type NodeDisconnected struct {
	BaseNodeEvent
	NodeId uintptr
}

func (e NodeDisconnected) EventType() string {
	return "NodeDisconnected"
}

// PublishStarted 발행 시작 이벤트
type PublishStarted struct {
	BaseNodeEvent
	NodeId uintptr
}

func (e PublishStarted) EventType() string {
	return "PublishStarted"
}

// PublishStopped 발행 중지 이벤트
type PublishStopped struct {
	BaseNodeEvent
	NodeId uintptr
}

func (e PublishStopped) EventType() string {
	return "PublishStopped"
}

// PlayStarted 재생 시작 이벤트
type PlayStarted struct {
	BaseNodeEvent
	NodeId uintptr
}

func (e PlayStarted) EventType() string {
	return "PlayStarted"
}

// PlayStopped 재생 중지 이벤트
type PlayStopped struct {
	BaseNodeEvent
	NodeId uintptr
}

func (e PlayStopped) EventType() string {
	return "PlayStopped"
}

// MediaDataReceived 미디어 데이터 수신 이벤트 (향후 확장용)
type MediaDataReceived struct {
	BaseNodeEvent
	NodeId    uintptr
	Timestamp uint32
	FrameType string
	DataSize  int
}

func (e MediaDataReceived) EventType() string {
	return "MediaDataReceived"
}

// SessionTerminated 세션 종료 이벤트
type SessionTerminated struct {
	SessionId string
	NodeType  MediaNodeType
}

func (e SessionTerminated) GetSessionId() string {
	return e.SessionId
}

func (e SessionTerminated) GetStreamName() string {
	return ""
}

func (e SessionTerminated) EventType() string {
	return "SessionTerminated"
}