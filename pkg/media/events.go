package media

// 모든 노드 이벤트의 기본 인터페이스
type NodeEvent interface {
	NodeId() uintptr
}

// 공통 노드 이벤트 필드
type BaseNodeEvent struct {
	ID       uintptr
	NodeType MediaNodeType
}

func (e BaseNodeEvent) NodeId() uintptr {
	return e.ID
}

// 노드 생성 이벤트 (연결된 상태)
type NodeCreated struct {
	BaseNodeEvent
	Node MediaNode // MediaNode 인스턴스
}

// 발행 시작 이벤트
type PublishStarted struct {
	BaseNodeEvent
	Stream *Stream
}

// 발행 중지 이벤트
type PublishStopped struct {
	BaseNodeEvent
	StreamId string
}

// 재생 시작 이벤트
type PlayStarted struct {
	BaseNodeEvent
	StreamId string
}

// 재생 중지 이벤트
type PlayStopped struct {
	BaseNodeEvent
	StreamId string
}

// 노드 종료 이벤트
type NodeTerminated struct {
	BaseNodeEvent
}
