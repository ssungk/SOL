package rtmp2

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestRTMP2Integration RTMP2 패키지 통합 테스트
func TestRTMP2Integration(t *testing.T) {
	// 스트림 매니저 생성
	streamManager := NewSimpleStreamManager()
	
	// 스트림 매니저 기본 기능 테스트
	testStreamId := "live/test_stream"
	stream := streamManager.GetOrCreateStream(testStreamId)
	if stream == nil {
		t.Fatal("Failed to create stream")
	}
	
	// 스트림 카운트 확인
	if streamManager.GetStreamCount() != 1 {
		t.Errorf("Expected 1 stream, got %d", streamManager.GetStreamCount())
	}

	t.Log("Stream manager basic functionality test passed")
	
	// 테스트 발행자 세션 정보
	publisherSessionInfo := RTMPSessionInfo{
		SessionID:  "test_publisher",
		StreamName: "test_stream",
		AppName:    "live",
		StreamID:   1,
		Type:       RTMPSessionTypePublisher,
	}

	// RTMP Source 생성 및 테스트 (로깅 제거)
	rtmpSource := NewRTMPSource(publisherSessionInfo, "test://publisher")
	
	if rtmpSource.GetSourceId() != "test_publisher" {
		t.Errorf("Expected source ID 'test_publisher', got '%s'", rtmpSource.GetSourceId())
	}
	
	if rtmpSource.IsActive() {
		t.Error("Source should not be active initially")
	}
	
	// 스트림에 연결
	rtmpSource.SetStream(stream)
	
	if !rtmpSource.IsActive() {
		t.Error("Source should be active after connecting to stream")
	}

	t.Log("RTMP Source functionality test passed")
	
	// RTMP Sink 테스트
	playerSessionInfo := RTMPSessionInfo{
		SessionID:  "test_player",
		StreamName: "test_stream", 
		AppName:    "live",
		StreamID:   1,
		Type:       RTMPSessionTypePlayer,
	}

	rtmpSink := NewRTMPSink(playerSessionInfo, nil, "test://player")
	
	if rtmpSink.GetSinkId() != "test_player" {
		t.Errorf("Expected sink ID 'test_player', got '%s'", rtmpSink.GetSinkId())
	}
	
	// 스트림에 싱크 추가
	if err := stream.AddSink(rtmpSink); err != nil {
		t.Fatalf("Failed to add sink to stream: %v", err)
	}
	
	// 스트림 상태 확인
	sinkCount := stream.GetSinkCount()
	if sinkCount != 1 {
		t.Errorf("Expected 1 sink, got %d", sinkCount)
	}

	t.Log("RTMP Sink functionality test passed")
	t.Log("RTMP2 integration test completed successfully")
}

// TestRTMPFrameConversion RTMP 프레임 변환 테스트
func TestRTMPFrameConversion(t *testing.T) {
	// 비디오 프레임 변환 테스트
	testData := [][]byte{{0x17, 0x00, 0x00, 0x00, 0x00}}
	
	videoFrame := convertRTMPFrameToMediaFrame(RTMPFrameTypeAVCSequenceHeader, 1000, testData, true)
	
	if videoFrame.FrameType != string(RTMPFrameTypeAVCSequenceHeader) {
		t.Errorf("Expected frame type %s, got %s", RTMPFrameTypeAVCSequenceHeader, videoFrame.FrameType)
	}
	
	if videoFrame.Timestamp != 1000 {
		t.Errorf("Expected timestamp 1000, got %d", videoFrame.Timestamp)
	}

	// 프레임 타입 확인 테스트
	if !isKeyFrame(RTMPFrameTypeKeyFrame) {
		t.Error("RTMPFrameTypeKeyFrame should be identified as key frame")
	}

	if !isSequenceHeader(RTMPFrameTypeAVCSequenceHeader) {
		t.Error("RTMPFrameTypeAVCSequenceHeader should be identified as sequence header")
	}

	if !isSequenceHeader(RTMPFrameTypeAACSequenceHeader) {
		t.Error("RTMPFrameTypeAACSequenceHeader should be identified as sequence header")
	}

	t.Log("RTMP frame conversion test completed successfully")
}

// ExampleRTMP2Server RTMP2 서버 사용 예제
func ExampleRTMP2Server() {
	// 스트림 매니저 생성
	streamManager := NewSimpleStreamManager()
	
	// 이벤트 채널 및 대기 그룹  
	eventChannel := make(chan interface{}, 100)
	var wg sync.WaitGroup
	
	// RTMP2 서버 설정
	streamConfig := RTMPStreamConfig{
		GopCacheSize:        10,
		MaxPlayersPerStream: 100,
	}
	
	// 서버 생성 및 시작
	server := NewServer(1935, streamConfig, streamManager, eventChannel, &wg)
	
	if err := server.Start(); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		return
	}
	defer server.Stop()
	
	fmt.Println("RTMP2 server started on port 1935")
	fmt.Println("You can now connect with:")
	fmt.Println("  Publisher: rtmp://localhost:1935/live/stream_key")
	fmt.Println("  Player: rtmp://localhost:1935/live/stream_key")
	
	// 이벤트 처리 (실제 서비스에서는 별도 고루틴에서 처리)
	go func() {
		for event := range eventChannel {
			fmt.Printf("Received event: %T\n", event)
		}
	}()
	
	// 서버 실행 (실제 서비스에서는 시그널 대기 등)
	time.Sleep(10 * time.Second)
	
	// Output: RTMP2 server started on port 1935
}