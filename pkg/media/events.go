package media


// 노드 생성 이벤트 (연결된 상태)
type NodeCreated struct {
	ID   uintptr
	Node MediaNode // MediaNode 인스턴스
}

// 노드 종료 이벤트
type NodeTerminated struct {
	ID uintptr
}

// 발행 시작 이벤트 (collision detection + 원자적 점유)
type PublishStarted struct {
	ID           uintptr
	Stream       *Stream
	ResponseChan chan<- Response
}

// 발행 중지 이벤트
type PublishStopped struct {
	ID       uintptr
	StreamID string
}

// 재생 시작 이벤트
type SubscribeStarted struct {
	ID              uintptr
	StreamID        string
	SupportedCodecs []Codec // Sink가 지원하는 코덱 목록
	ResponseChan    chan<- Response
}

// 재생 중지 이벤트
type SubscribeStopped struct {
	ID       uintptr
	StreamID string
}

// MediaServer 범용 응답 (success/failure + error message)
type Response struct {
	Success bool
	Error   string
}

// NewResponse Response 생성자
func NewResponse(success bool, error string) Response {
	return Response{
		Success: success,
		Error:   error,
	}
}

// NewSuccessResponse 성공 응답 생성자
func NewSuccessResponse() Response {
	return NewResponse(true, "")
}

// NewErrorResponse 실패 응답 생성자
func NewErrorResponse(error string) Response {
	return NewResponse(false, error)
}

// 이벤트 생성자 함수들

// NewNodeCreated 노드 생성 이벤트 생성자
func NewNodeCreated(id uintptr, node MediaNode) NodeCreated {
	return NodeCreated{
		ID:   id,
		Node: node,
	}
}

// NewNodeTerminated 노드 종료 이벤트 생성자
func NewNodeTerminated(id uintptr) NodeTerminated {
	return NodeTerminated{
		ID: id,
	}
}

// NewPublishStarted 발행 시작 이벤트 생성자
func NewPublishStarted(id uintptr, stream *Stream, responseChan chan<- Response) PublishStarted {
	return PublishStarted{
		ID:           id,
		Stream:       stream,
		ResponseChan: responseChan,
	}
}

// NewPublishStopped 발행 중지 이벤트 생성자
func NewPublishStopped(id uintptr, streamID string) PublishStopped {
	return PublishStopped{
		ID:       id,
		StreamID: streamID,
	}
}

// NewSubscribeStarted 재생 시작 이벤트 생성자
func NewSubscribeStarted(id uintptr, streamID string, supportedCodecs []Codec, responseChan chan<- Response) SubscribeStarted {
	return SubscribeStarted{
		ID:              id,
		StreamID:        streamID,
		SupportedCodecs: supportedCodecs,
		ResponseChan:    responseChan,
	}
}

// NewSubscribeStopped 재생 중지 이벤트 생성자
func NewSubscribeStopped(id uintptr, streamID string) SubscribeStopped {
	return SubscribeStopped{
		ID:       id,
		StreamID: streamID,
	}
}
