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

// 이벤트 생성자 함수들

// NewNodeCreated 노드 생성 이벤트 생성자
func NewNodeCreated(id uintptr, nodeType MediaNodeType, node MediaNode) NodeCreated {
	return NodeCreated{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		Node:          node,
	}
}

// NewPublishStarted 발행 시작 이벤트 생성자
func NewPublishStarted(id uintptr, nodeType MediaNodeType, stream *Stream) PublishStarted {
	return PublishStarted{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		Stream:        stream,
	}
}

// NewPublishStopped 발행 중지 이벤트 생성자
func NewPublishStopped(id uintptr, nodeType MediaNodeType, streamId string) PublishStopped {
	return PublishStopped{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		StreamId:      streamId,
	}
}

// NewPlayStarted 재생 시작 이벤트 생성자
func NewPlayStarted(id uintptr, nodeType MediaNodeType, streamId string) PlayStarted {
	return PlayStarted{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		StreamId:      streamId,
	}
}

// NewPlayStopped 재생 중지 이벤트 생성자
func NewPlayStopped(id uintptr, nodeType MediaNodeType, streamId string) PlayStopped {
	return PlayStopped{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		StreamId:      streamId,
	}
}

// NewNodeTerminated 노드 종료 이벤트 생성자
func NewNodeTerminated(id uintptr, nodeType MediaNodeType) NodeTerminated {
	return NodeTerminated{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
	}
}
