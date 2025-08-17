package media

// 모든 노드 이벤트의 기본 인터페이스
type NodeEvent interface {
	NodeId() uintptr
}

// 공통 노드 이벤트 필드
type BaseNodeEvent struct {
	ID       uintptr
	NodeType NodeType
}

func (e BaseNodeEvent) NodeId() uintptr {
	return e.ID
}

// 노드 생성 이벤트 (연결된 상태)
type NodeCreated struct {
	BaseNodeEvent
	Node MediaNode // MediaNode 인스턴스
}

// 노드 종료 이벤트
type NodeTerminated struct {
	BaseNodeEvent
}

// 발행 시작 이벤트 (collision detection + 원자적 점유)
type PublishStarted struct {
	BaseNodeEvent
	Stream       *Stream
	ResponseChan chan<- Response
}

// 발행 중지 이벤트
type PublishStopped struct {
	BaseNodeEvent
	StreamId string
}

// 재생 시작 이벤트
type SubscribeStarted struct {
	BaseNodeEvent
	StreamId string
}

// 재생 중지 이벤트
type SubscribeStopped struct {
	BaseNodeEvent
	StreamId string
}

// MediaServer 범용 응답 (success/failure + error message)
type Response struct {
	Success bool
	Error   string
}

// 이벤트 생성자 함수들

// NewNodeCreated 노드 생성 이벤트 생성자
func NewNodeCreated(id uintptr, nodeType NodeType, node MediaNode) NodeCreated {
	return NodeCreated{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		Node:          node,
	}
}

// NewNodeTerminated 노드 종료 이벤트 생성자
func NewNodeTerminated(id uintptr, nodeType NodeType) NodeTerminated {
	return NodeTerminated{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
	}
}

// NewPublishStarted 발행 시작 이벤트 생성자
func NewPublishStarted(id uintptr, nodeType NodeType, stream *Stream, responseChan chan<- Response) PublishStarted {
	return PublishStarted{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		Stream:        stream,
		ResponseChan:  responseChan,
	}
}

// NewPublishStopped 발행 중지 이벤트 생성자
func NewPublishStopped(id uintptr, nodeType NodeType, streamId string) PublishStopped {
	return PublishStopped{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		StreamId:      streamId,
	}
}

// NewSubscribeStarted 재생 시작 이벤트 생성자
func NewSubscribeStarted(id uintptr, nodeType NodeType, streamId string) SubscribeStarted {
	return SubscribeStarted{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		StreamId:      streamId,
	}
}

// NewSubscribeStopped 재생 중지 이벤트 생성자
func NewSubscribeStopped(id uintptr, nodeType NodeType, streamId string) SubscribeStopped {
	return SubscribeStopped{
		BaseNodeEvent: BaseNodeEvent{ID: id, NodeType: nodeType},
		StreamId:      streamId,
	}
}
