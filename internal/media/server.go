package media

import (
	"context"
	"fmt"
	"log/slog"
	"sol/pkg/hls"
	"sol/pkg/media"
	"sol/pkg/rtmp"
	"sol/pkg/rtsp"
	"sol/pkg/utils"
	"sync"
)

// represents stream configuration
type StreamConfig struct {
	GopCacheSize        int
	MaxPlayersPerStream int
}

type MediaServer struct {
	servers  map[string]ProtocolServer   // serverName -> ProtocolServer (rtmp, rtsp, hls)
	mediaServerChannel  chan any
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // 송신자들을 추적하기 위한 WaitGroup
	
	// 통합 스트림 및 노드 관리
	streams map[string]*media.Stream     // streamId -> Stream
	nodes   map[uintptr]media.MediaNode  // nodeId -> MediaNode (Source|Sink)
	streamSources map[string]uintptr     // streamId -> sourceNodeId (소스 추적용)
}

func NewMediaServer(rtmpPort, rtspPort, rtspTimeout int, hlsConfig hls.HLSConfig, streamConfig StreamConfig) *MediaServer {
	// 자체적으로 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	mediaServer := &MediaServer{
		servers: make(map[string]ProtocolServer),
		mediaServerChannel: make(chan any, media.DefaultChannelBufferSize),
		ctx:     ctx,
		cancel:  cancel,
		streams: make(map[string]*media.Stream),
		nodes:   make(map[uintptr]media.MediaNode),
		streamSources: make(map[string]uintptr),
	}

	// 각 서버를 생성하고 맵에 등록
	rtmpServer := rtmp.NewServer(rtmpPort, mediaServer.mediaServerChannel, &mediaServer.wg)
	mediaServer.servers["rtmp"] = rtmpServer

	rtspServer := rtsp.NewServer(rtsp.NewRTSPConfig(rtspPort, rtspTimeout), mediaServer.mediaServerChannel, &mediaServer.wg)
	mediaServer.servers["rtsp"] = rtspServer
	
	hlsServer := hls.NewServer(hlsConfig, mediaServer.mediaServerChannel, &mediaServer.wg)
	mediaServer.servers["hls"] = hlsServer
	
	return mediaServer
}

func (s *MediaServer) Start() error {
	slog.Info("Media servers starting...")

	// 모든 서버들을 순차적으로 시작
	for name, server := range s.servers {
		if err := server.Start(); err != nil {
			slog.Error("Failed to start server", "serverName", name, "err", err)
			return fmt.Errorf("failed to start %s server: %w", name, err)
		}
		slog.Info("Server started", "serverName", name, "protocol", server.Name())
	}

	// 이벤트 루프 시작
	go s.eventLoop()

	return nil
}

// stops the server
func (s *MediaServer) Stop() {
	slog.Info("Stopping Media Server...")
	s.cancel()
	slog.Info("Media Server stop signal sent")
}

func (s *MediaServer) eventLoop() {
	for {
		select {
		case data := <-s.mediaServerChannel:
			s.channelHandler(data)
		case <-s.ctx.Done():
			s.shutdown()
			return
		}
	}
}

func (s *MediaServer) channelHandler(data any) {
	switch v := data.(type) {
	// 공통 이벤트 처리
	case media.PublishStopped:
		s.handlePublishStopped(v)
	case media.PlayStarted:
		s.handlePlayStarted(v)
	case media.PlayStopped:
		s.handlePlayStopped(v)
	case media.NodeCreated:
		s.handleNodeCreated(v)
	case media.NodeTerminated:
		s.handleNodeTerminated(v)
	case media.PublishStarted:
		s.handleStreamPublishAttempt(v)
	default:
		slog.Warn("Unknown event type", "eventType", utils.TypeName(v))
	}
}

// shutdown performs the actual shutdown sequence
func (s *MediaServer) shutdown() {
	slog.Info("Media event loop stopping...")

	// 모든 서버들을 순차적으로 중지
	for name, server := range s.servers {
		server.Stop()
		slog.Info("Server stopped", "serverName", name)
	}

	// 모든 송신자(eventLoop)가 완료될 때까지 대기
	s.wg.Wait()
	slog.Info("All senders finished")

	slog.Info("Media Server stopped successfully")
}

// 스트림을 가져오거나 생성
func (s *MediaServer) GetOrCreateStream(streamId string) *media.Stream {
	stream, exists := s.streams[streamId]
	if !exists {
		stream = media.NewStream(streamId)
		s.streams[streamId] = stream
		slog.Info("Created new stream", "streamId", streamId)
	}
	
	return stream
}

// 노드(Source 또는 Sink) 등록
func (s *MediaServer) RegisterNode(streamId string, node media.MediaNode) {
	nodeId := node.ID()
	
	// 이미 nodes 맵에 있다면 스킵 (NodeCreated에서 이미 등록됨)
	if _, exists := s.nodes[nodeId]; !exists {
		s.nodes[nodeId] = node
	}
	
	// 스트림 가져오거나 생성
	stream := s.GetOrCreateStream(streamId)
	
	// 노드 타입에 따라 처리
	if source, ok := node.(media.MediaSource); ok {
		// Source 등록 - 스트림에 연결은 프로토콜별로 처리
		slog.Info("Registered source", "streamId", streamId, "nodeId", nodeId, "nodeType", node.NodeType())
		_ = source // 사용 표시
	}
	
	if sink, ok := node.(media.MediaSink); ok {
		// Sink 등록 - 스트림에 추가
		if err := stream.AddSink(sink); err != nil {
			slog.Error("Failed to add sink to stream", "streamId", streamId, "nodeId", nodeId, "err", err)
			return
		}
		
		// 캐시된 데이터 전송
		if err := stream.SendCachedDataToSink(sink); err != nil {
			slog.Error("Failed to send cached data to sink", "streamId", streamId, "nodeId", nodeId, "err", err)
		}
		
		slog.Info("Registered sink", "streamId", streamId, "nodeId", nodeId, "nodeType", node.NodeType())
	}
}

// 노드 제거
func (s *MediaServer) RemoveNode(nodeId uintptr) {
	node, exists := s.nodes[nodeId]
	if !exists {
		return
	}
	
	// 노드를 포함하는 스트림 찾기 및 제거
	var targetStreamIds []string
	
	// Source(발행자) 노드인 경우 처리
	if _, ok := node.(media.MediaSource); ok {
		// 이 소스가 어떤 스트림들을 만들었는지 찾기
		for streamId, sourceNodeId := range s.streamSources {
			if sourceNodeId == nodeId {
				targetStreamIds = append(targetStreamIds, streamId)
				// 소스 맵핑도 제거
				delete(s.streamSources, streamId)
				slog.Info("Source node removed, marking stream for cleanup", "nodeId", nodeId, "streamId", streamId)
			}
		}
	}
	
	// Sink(재생자) 노드인 경우 처리
	if _, ok := node.(media.MediaSink); ok {
		for streamId, stream := range s.streams {
			for _, existingSink := range stream.GetSinks() {
				if existingSink.ID() == nodeId {
					stream.RemoveSink(existingSink)
					targetStreamIds = append(targetStreamIds, streamId)
					slog.Info("Sink node removed from stream", "nodeId", nodeId, "streamId", streamId)
					break
				}
			}
		}
	}
	
	// 노드 제거
	delete(s.nodes, nodeId)
	
	slog.Info("Removed node", "nodeId", nodeId, "streamIds", targetStreamIds, "nodeType", node.NodeType())
	
	// 스트림 정리 로직 개선
	for _, streamId := range targetStreamIds {
		if stream := s.streams[streamId]; stream != nil {
			// Source가 제거된 경우 또는 Sink가 모두 제거된 경우 스트림 삭제
			// 소스가 제거된 스트림인지 확인
			_, wasSourceStream := s.streamSources[streamId]
			if _, wasSource := node.(media.MediaSource); wasSource && wasSourceStream {
				// Source가 제거되면 스트림 무조건 삭제
				stream.Stop()
				delete(s.streams, streamId)
				// streamSources 맵핑도 제거 (이미 위에서 제거되었을 수도 있음)
				delete(s.streamSources, streamId)
				slog.Info("Removed stream due to source removal", "streamId", streamId)
			} else if stream.GetSinkCount() == 0 {
				// Sink만 제거된 경우, 다른 Sink가 없으면 스트림 삭제
				stream.Stop()
				delete(s.streams, streamId)
				// 혹시 소스 맵핑도 제거
				delete(s.streamSources, streamId)
				slog.Info("Removed stream due to no remaining sinks", "streamId", streamId)
			}
		}
	}
}

// 스트림 개수 반환
func (s *MediaServer) GetStreamCount() int {
	return len(s.streams)
}

// 노드 개수 반환  
func (s *MediaServer) GetNodeCount() int {
	return len(s.nodes)
}

// 스트림 통계 반환
func (s *MediaServer) GetStreamStats() map[string]any {
	sourceCount := 0
	sinkCount := 0
	
	for _, node := range s.nodes {
		if _, ok := node.(media.MediaSource); ok {
			sourceCount++
		}
		if _, ok := node.(media.MediaSink); ok {
			sinkCount++
		}
	}
	
	return map[string]any{
		"total_streams": len(s.streams),
		"total_nodes":   len(s.nodes),
		"source_count":  sourceCount,
		"sink_count":    sinkCount,
	}
}

// 공통 이벤트 핸들러들

// getHLSServer HLS 서버 반환
func (s *MediaServer) getHLSServer() (*hls.Server, bool) {
	server, exists := s.servers["hls"]
	if !exists {
		return nil, false
	}
	hlsServer, ok := server.(*hls.Server)
	return hlsServer, ok
}

// RegisterServer 새 서버를 동적으로 등록
func (s *MediaServer) RegisterServer(name string, server ProtocolServer) error {
	if _, exists := s.servers[name]; exists {
		return fmt.Errorf("server %s already exists", name)
	}
	
	s.servers[name] = server
	slog.Info("Server registered", "serverName", name, "protocol", server.Name())
	return nil
}

// UnregisterServer 서버를 동적으로 제거
func (s *MediaServer) UnregisterServer(name string) error {
	server, exists := s.servers[name]
	if !exists {
		return fmt.Errorf("server %s not found", name)
	}
	
	// 서버 중지 (멱등적이므로 상태 체크 불필요)
	server.Stop()
	slog.Info("Server stopped during unregistration", "serverName", name)
	
	delete(s.servers, name)
	slog.Info("Server unregistered", "serverName", name)
	return nil
}

// GetServer 서버 반환
func (s *MediaServer) GetServer(name string) (ProtocolServer, bool) {
	server, exists := s.servers[name]
	return server, exists
}

// GetServerNames 등록된 모든 서버 이름 반환
func (s *MediaServer) GetServerNames() []string {
	names := make([]string, 0, len(s.servers))
	for name := range s.servers {
		names = append(names, name)
	}
	return names
}


// CreateHLSStream HLS 스트림 생성
func (s *MediaServer) CreateHLSStream(streamID string) error {
	hlsServer, ok := s.getHLSServer()
	if !ok {
		return fmt.Errorf("HLS server not found")
	}
	
	if err := hlsServer.CreateStream(streamID); err != nil {
		return fmt.Errorf("failed to create HLS stream: %w", err)
	}
	
	// HLS 패키저를 MediaSink으로 등록
	packager, exists := hlsServer.GetPackager(streamID)
	if exists {
		s.RegisterNode(streamID, packager)
		slog.Info("HLS packager registered as MediaSink", "streamID", streamID)
	}
	
	return nil
}

// RemoveHLSStream HLS 스트림 제거
func (s *MediaServer) RemoveHLSStream(streamID string) {
	hlsServer, ok := s.getHLSServer()
	if !ok {
		slog.Warn("HLS server not found for stream removal", "streamID", streamID)
		return
	}
	
	hlsServer.RemoveStream(streamID)
	slog.Info("HLS stream removed", "streamID", streamID)
}

// handlePublishStarted 발행 시작 이벤트 처리
func (s *MediaServer) handlePublishStarted(event media.PublishStarted) {
	// 노드 ID를 통해 노드 찾기
	node, exists := s.nodes[event.NodeId()]
	if !exists {
		slog.Error("Node not found for publish started", "nodeId", event.NodeId())
		return
	}
	
	// 소스인지 확인
	_, ok := node.(media.MediaSource)
	if !ok {
		slog.Error("Node is not a source", "nodeId", event.NodeId())
		return
	}
	
	var stream *media.Stream
	var streamId string
	
	// RTSP 노드의 경우 스트림 생성 및 연결
	if event.NodeType == media.NodeTypeRTSP {
		if rtspSession, ok := node.(*rtsp.Session); ok {
			streamId = rtspSession.GetStreamPath()
			stream = s.GetOrCreateStream(streamId)
			rtspSession.Stream = stream
			slog.Info("Stream created and connected for RTSP publish", "streamId", streamId, "sessionId", rtspSession.GetStreamPath())
		}
	} else if event.Stream != nil {
		// RTMP 등 다른 프로토콜의 경우 이미 생성된 스트림 사용
		stream = event.Stream
		streamId = stream.ID()
	} else {
		slog.Error("No stream provided and unable to create for node type", "nodeType", event.NodeType.String())
		return
	}
	
	// 스트림을 MediaServer에 등록
	s.streams[streamId] = stream
	// 소스 맵핑 등록
	s.streamSources[streamId] = event.NodeId()
	
	// HLS 스트림 자동 생성 비활성화됨 (수동으로 API를 통해 생성 필요)
	// if err := s.CreateHLSStream(streamId); err != nil {
	// 	slog.Warn("Failed to create HLS stream", "streamId", streamId, "err", err)
	// } else {
	// 	slog.Info("HLS stream created for publish", "streamId", streamId)
	// }
	
	slog.Info("Source registered for publish", "streamId", streamId, "sourceId", event.NodeId(), "nodeType", event.NodeType.String())
}

// handlePublishStopped 발행 중지 이벤트 처리  
func (s *MediaServer) handlePublishStopped(event media.PublishStopped) {
	slog.Info("Publish stopped", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())
	
	// 중복 처리 방지: 노드가 아직 존재하는 경우만 처리
	if _, exists := s.nodes[event.NodeId()]; exists {
		// HLS 스트림 자동 제거 비활성화됨 (수동으로 API를 통해 관리 필요)
		// s.RemoveHLSStream(event.StreamId)
		
		// Source 노드 제거 및 스트림 정리
		s.RemoveNode(event.NodeId())
		slog.Info("Source node removed due to publish stop", "streamId", event.StreamId, "sourceId", event.NodeId())
	} else {
		slog.Debug("Publish stopped event for already removed node", "nodeId", event.NodeId())
	}
}

// handlePlayStarted 재생 시작 이벤트 처리
func (s *MediaServer) handlePlayStarted(event media.PlayStarted) {
	slog.Info("Play started", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())
	
	// 노드 ID를 통해 노드 찾기
	node, exists := s.nodes[event.NodeId()]
	if !exists {
		slog.Error("Node not found for play started", "nodeId", event.NodeId())
		return
	}
	
	// 싱크인지 확인
	sink, ok := node.(media.MediaSink)
	if !ok {
		slog.Error("Node is not a sink", "nodeId", event.NodeId())
		return
	}
	
	// 스트림 가져오거나 생성
	stream := s.GetOrCreateStream(event.StreamId)
	
	// Sink를 스트림에 추가
	if err := stream.AddSink(sink); err != nil {
		slog.Error("Failed to add sink to stream", "streamId", event.StreamId, "sinkId", event.NodeId(), "err", err)
		return
	}
	
	// RTSP 세션인 경우 스트림 참조 설정
	if event.NodeType == media.NodeTypeRTSP {
		if rtspSession, ok := sink.(*rtsp.Session); ok {
			rtspSession.Stream = stream
			slog.Info("Stream reference set for RTSP session", "streamId", event.StreamId, "sessionId", rtspSession.GetStreamPath())
		}
	}
	
	// 캐시된 데이터 전송
	if err := stream.SendCachedDataToSink(sink); err != nil {
		slog.Error("Failed to send cached data to sink", "streamId", event.StreamId, "sinkId", event.NodeId(), "err", err)
	}
	
	slog.Info("Sink registered for play", "streamId", event.StreamId, "sinkId", event.NodeId())
}

// handlePlayStopped 재생 중지 이벤트 처리
func (s *MediaServer) handlePlayStopped(event media.PlayStopped) {
	slog.Info("Play stopped", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())
	
	// 중복 처리 방지: 노드가 아직 존재하는 경우만 처리
	if _, exists := s.nodes[event.NodeId()]; exists {
		// Sink 노드 제거 및 스트림 정리
		s.RemoveNode(event.NodeId())
		slog.Info("Sink node removed due to play stop", "streamId", event.StreamId, "sinkId", event.NodeId())
	} else {
		slog.Debug("Play stopped event for already removed node", "nodeId", event.NodeId())
	}
}

// handleNodeCreated 노드 생성 이벤트 처리 (연결만 된 상태)
func (s *MediaServer) handleNodeCreated(event media.NodeCreated) {
	slog.Info("Node created", "nodeId", event.NodeId(), "nodeType", event.NodeType.String())
	
	// 노드를 nodes 맵에 등록 (아직 스트림에는 연결하지 않음)
	s.nodes[event.NodeId()] = event.Node
	slog.Info("Node registered in nodes map", "nodeId", event.NodeId())
}


// handleNodeTerminated 노드 종료 이벤트 처리
func (s *MediaServer) handleNodeTerminated(event media.NodeTerminated) {
	slog.Info("Node terminated", "nodeId", event.NodeId(), "nodeType", event.NodeType.String())
	
	// 노드 제거 및 정리
	s.RemoveNode(event.NodeId())
}

// handleStreamPublishAttempt 실제 publish 시도 처리 (collision detection + 원자적 점유)
func (s *MediaServer) handleStreamPublishAttempt(event media.PublishStarted) {
	streamId := event.Stream.ID()
	slog.Debug("Stream publish attempt requested", "streamId", streamId, "nodeId", event.NodeId())
	
	response := media.Response{
		Success: false,
		Error:   "",
	}
	
	// 최종 확인: 아직 사용 중이지 않은지 체크
	if _, exists := s.streams[streamId]; exists {
		response.Error = "Stream ID was taken by another client"
		slog.Info("Stream publish failed - ID taken", "streamId", streamId, "nodeId", event.NodeId())
	} else {
		// 원자적으로 스트림 점유
		s.streams[streamId] = event.Stream
		s.streamSources[streamId] = event.NodeId()
		response.Success = true
		slog.Info("Stream publish successful", "streamId", streamId, "nodeId", event.NodeId())
	}
	
	// 응답 전송 (채널이 닫혀있어도 panic 방지)
	select {
	case event.ResponseChan <- response:
		slog.Debug("Stream publish attempt response sent", "streamId", streamId, "success", response.Success)
	default:
		slog.Warn("Failed to send stream publish attempt response - channel closed", "streamId", streamId)
		// 만약 성공했는데 응답을 못보냈다면 정리해야 함
		if response.Success {
			delete(s.streams, streamId)
			delete(s.streamSources, streamId)
			slog.Warn("Rolled back successful stream publish due to closed response channel", "streamId", streamId)
		}
	}
}
