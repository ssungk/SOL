package media

import (
	"context"
	"fmt"
	"log/slog"
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
	servers            map[string]media.ServerInterface // serverName -> ServerInterface (rtmp, rtsp)
	mediaServerChannel chan any
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup // 송신자들을 추적하기 위한 WaitGroup

	// 통합 스트림 및 노드 관리
	streams map[string]*media.Stream    // streamId -> Stream
	nodes   map[uintptr]media.MediaNode // nodeId -> MediaNode (Source|Sink)
}

func NewMediaServer(rtmpPort, rtspPort, rtspTimeout int, streamConfig StreamConfig) *MediaServer {
	// 자체적으로 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	mediaServer := &MediaServer{
		servers:            make(map[string]media.ServerInterface),
		mediaServerChannel: make(chan any, media.DefaultChannelBufferSize),
		ctx:                ctx,
		cancel:             cancel,
		streams:            make(map[string]*media.Stream),
		nodes:              make(map[uintptr]media.MediaNode),
	}

	// 각 서버를 생성하고 맵에 등록
	rtmpServer := rtmp.NewServer(rtmpPort, mediaServer.mediaServerChannel, &mediaServer.wg)
	mediaServer.servers[rtmpServer.ID()] = rtmpServer

	rtspServer := rtsp.NewServer(rtsp.NewRTSPConfig(rtspPort, rtspTimeout), mediaServer.mediaServerChannel, &mediaServer.wg)
	mediaServer.servers[rtspServer.ID()] = rtspServer

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
		slog.Info("Server started", "serverName", name, "protocol", server.ID())
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
			s.handleChannel(data)
		case <-s.ctx.Done():
			s.shutdown()
			return
		}
	}
}

func (s *MediaServer) handleChannel(data any) {
	switch v := data.(type) {
	// 노드 라이프사이클 이벤트
	case media.NodeCreated:
		s.handleNodeCreated(v)
	case media.NodeTerminated:
		s.handleNodeTerminated(v)
	// 스트림 발행 이벤트
	case media.PublishStarted:
		s.handlePublishStarted(v)
	case media.PublishStopped:
		s.handlePublishStopped(v)
	// 스트림 재생 이벤트
	case media.SubscribeStarted:
		s.handleSubscribeStarted(v)
	case media.SubscribeStopped:
		s.handleSubscribeStopped(v)
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

// 노드 제거
func (s *MediaServer) RemoveNode(nodeId uintptr) {
	node, exists := s.nodes[nodeId]
	if !exists {
		slog.Debug("Node not found for removal", "nodeId", nodeId)
		return
	}

	// Sink 노드인 경우 모든 스트림에서 제거
	if _, ok := node.(media.MediaSink); ok {
		slog.Debug("Processing MediaSink node removal", "nodeId", nodeId)
		for streamId, stream := range s.streams {
			for _, existingSink := range stream.GetSinks() {
				if existingSink.ID() == nodeId {
					stream.RemoveSink(existingSink)
					slog.Info("Sink node removed from stream", "nodeId", nodeId, "streamId", streamId)
					break
				}
			}
		}
	}

	// 노드 제거
	delete(s.nodes, nodeId)
	slog.Info("Node removed", "nodeId", nodeId, "nodeType", node.NodeType(), "remainingNodes", len(s.nodes))
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

// RegisterServer 새 서버를 동적으로 등록
func (s *MediaServer) RegisterServer(name string, server media.ServerInterface) error {
	if _, exists := s.servers[name]; exists {
		return fmt.Errorf("server %s already exists", name)
	}

	s.servers[name] = server
	slog.Info("Server registered", "serverName", name, "protocol", server.ID())
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
func (s *MediaServer) GetServer(name string) (media.ServerInterface, bool) {
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

// handleNodeCreated 노드 생성 이벤트 처리
func (s *MediaServer) handleNodeCreated(event media.NodeCreated) {
	slog.Info("Node created", "nodeId", event.NodeId(), "nodeType", event.NodeType.String())
	s.nodes[event.NodeId()] = event.Node
	slog.Info("Node registered in nodes map", "nodeId", event.NodeId())
}

// handleNodeTerminated 노드 종료 이벤트 처리
func (s *MediaServer) handleNodeTerminated(event media.NodeTerminated) {
	slog.Info("Node terminated", "nodeId", event.NodeId(), "nodeType", event.NodeType.String())
	s.RemoveNode(event.NodeId())
}

// handlePublishStarted 실제 publish 시도 처리
func (s *MediaServer) handlePublishStarted(event media.PublishStarted) {
	streamId := event.Stream.ID()
	slog.Debug("Stream publish attempt requested", "streamId", streamId, "nodeId", event.NodeId())

	var response media.Response

	// 최종 확인: 아직 사용 중이지 않은지 체크
	if _, exists := s.streams[streamId]; exists {
		response = media.NewErrorResponse("Stream ID was taken by another client")
		slog.Info("Stream publish failed - ID taken", "streamId", streamId, "nodeId", event.NodeId())
	} else {
		// 원자적으로 스트림 점유
		s.streams[streamId] = event.Stream
		response = media.NewSuccessResponse()
		slog.Info("Stream publish successful", "streamId", streamId, "nodeId", event.NodeId())
	}

	// 응답 전송 (버퍼가 있어서 항상 성공)
	event.ResponseChan <- response
	slog.Debug("Stream publish attempt response sent", "streamId", streamId, "success", response.Success)
}

// handlePublishStopped 발행 중지 이벤트 처리
func (s *MediaServer) handlePublishStopped(event media.PublishStopped) {
	slog.Info("Publish stopped", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())

	// 스트림 직접 삭제
	if stream, exists := s.streams[event.StreamId]; exists {
		stream.Stop()
		delete(s.streams, event.StreamId)
		slog.Info("Stream removed due to publish stop", "streamId", event.StreamId)
	}

	// 노드 제거
	if _, exists := s.nodes[event.NodeId()]; exists {
		s.RemoveNode(event.NodeId())
		slog.Info("Source node removed due to publish stop", "nodeId", event.NodeId())
	} else {
		slog.Debug("Publish stopped event for already removed node", "nodeId", event.NodeId())
	}
}

// handleSubscribeStarted 재생 시작 이벤트 처리
func (s *MediaServer) handleSubscribeStarted(event media.SubscribeStarted) {
	slog.Debug("Subscribe attempt requested", "streamId", event.StreamId, "nodeId", event.NodeId())

	// 응답 전송 헬퍼 함수
	sendResponse := func(success bool, error string) {
		if event.ResponseChan != nil {
			response := media.NewResponse(success, error)
			select {
			case event.ResponseChan <- response:
				slog.Debug("Subscribe response sent", "streamId", event.StreamId, "nodeId", event.NodeId(), "success", success)
			default:
				slog.Warn("Subscribe response channel full", "streamId", event.StreamId, "nodeId", event.NodeId())
			}
		}
	}

	// 노드 ID를 통해 노드 찾기
	node, exists := s.nodes[event.NodeId()]
	if !exists {
		sendResponse(false, "Node not found")
		slog.Error("Node not found for subscribe started", "nodeId", event.NodeId())
		return
	}

	// sink인지 확인
	sink, ok := node.(media.MediaSink)
	if !ok {
		sendResponse(false, "Node is not a sink")
		slog.Error("Node is not a sink", "nodeId", event.NodeId())
		return
	}

	// 스트림 존재 확인 (Subscribe는 기존 스트림에만 가능)
	stream, exists := s.streams[event.StreamId]
	if !exists {
		sendResponse(false, "Stream not found")
		slog.Error("Stream not found for subscribe", "streamId", event.StreamId, "nodeId", event.NodeId())
		return
	}

	// Sink를 스트림에 추가 (캐시된 데이터 자동 전송 포함)
	if err := stream.AddSink(sink); err != nil {
		sendResponse(false, fmt.Sprintf("Failed to add sink to stream: %v", err))
		slog.Error("Failed to add sink to stream", "streamId", event.StreamId, "sinkId", event.NodeId(), "err", err)
		return
	}

	// 성공 응답 (이제 RTMP 세션이 이미 매핑을 설정했으므로 안전)
	sendResponse(true, "")

	// RTSP 세션인 경우 스트림 참조 설정
	if event.NodeType == media.NodeTypeRTSP {
		if rtspSession, ok := sink.(*rtsp.Session); ok {
			rtspSession.Stream = stream
			slog.Info("Stream reference set for RTSP session", "streamId", event.StreamId, "sessionId", rtspSession.GetStreamPath())
		}
	}

	slog.Info("Sink registered for subscribe", "streamId", event.StreamId, "sinkId", event.NodeId(), "nodeType", event.NodeType.String())
}

// handleSubscribeStopped 재생 중지 이벤트 처리
func (s *MediaServer) handleSubscribeStopped(event media.SubscribeStopped) {
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
