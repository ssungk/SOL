package media

// Stream 내부 이벤트 타입들 (MediaServer 패턴과 동일)

// sendFrameEvent 프레임 전송 이벤트
type sendFrameEvent struct {
	frame Frame
}

// sendMetadataEvent 메타데이터 전송 이벤트
type sendMetadataEvent struct {
	metadata map[string]string
}

// addSinkEvent Sink 추가 이벤트
type addSinkEvent struct {
	sink MediaSink
}

// removeSinkEvent Sink 제거 이벤트
type removeSinkEvent struct {
	sink MediaSink
}

// stopStreamEvent 스트림 정지 이벤트
type stopStreamEvent struct{}