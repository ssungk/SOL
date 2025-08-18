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
}

func (s *MediaServer) eventLoop() {
	defer s.shutdown()
	for {
		select {
		case data := <-s.mediaServerChannel:
			s.handleChannel(data)
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *MediaServer) handleChannel(data any) {
	switch v := data.(type) {
	case media.NodeCreated:
		s.handleNodeCreated(v)
	case media.NodeTerminated:
		s.handleNodeTerminated(v)
	case media.PublishStarted:
		s.handlePublishStarted(v)
	case media.PublishStopped:
		s.handlePublishStopped(v)
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

	s.wg.Wait()
	slog.Info("Media Server stopped successfully")
}

// AddNode 노드 추가
func (s *MediaServer) AddNode(nodeId uintptr, node media.MediaNode) {
	s.nodes[nodeId] = node
	slog.Info("Node added", "nodeId", nodeId, "nodeType", node.NodeType(), "totalNodes", len(s.nodes))
}

// RemoveNode 노드 제거
func (s *MediaServer) RemoveNode(nodeId uintptr) {
	node, exists := s.nodes[nodeId]
	if !exists {
		slog.Debug("Node not found for removal", "nodeId", nodeId)
		return
	}

	// 외부에서 의도적으로 노드 종료 시 Close 호출하여 정리 작업 보장
	utils.CloseWithLog(node)

	// 노드 제거
	delete(s.nodes, nodeId)
	slog.Info("Node removed", "nodeId", nodeId, "nodeType", node.NodeType(), "remainingNodes", len(s.nodes))
}

// handleNodeCreated 노드 생성 이벤트 처리
func (s *MediaServer) handleNodeCreated(event media.NodeCreated) {
	slog.Info("Node created", "nodeId", event.NodeId(), "nodeType", event.NodeType.String())
	s.AddNode(event.NodeId(), event.Node)
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

	if _, exists := s.streams[streamId]; exists {
		event.ResponseChan <- media.NewErrorResponse("Stream ID was taken by another node")
		return
	}

	s.streams[streamId] = event.Stream
	event.ResponseChan <- media.NewSuccessResponse()
	slog.Info("Stream publish successful", "streamId", streamId, "nodeId", event.NodeId())
}

// handlePublishStopped 발행 중지 이벤트 처리
func (s *MediaServer) handlePublishStopped(event media.PublishStopped) {
	slog.Info("Publish stopped", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())

	if stream, exists := s.streams[event.StreamId]; exists {
		// TODO: stream관련해서 고루틴 동시접근 문제가 있을것같다 내부에 고루틴과 이벤트루프 추가해서 동시접근 안되도록 개선이 필요할듯
		stream.Stop()
		delete(s.streams, event.StreamId)
		slog.Info("Stream removed due to publish stop", "streamId", event.StreamId)
	}
}

// handleSubscribeStarted 재생 시작 이벤트 처리
func (s *MediaServer) handleSubscribeStarted(event media.SubscribeStarted) {
	slog.Debug("Subscribe attempt requested", "streamId", event.StreamId, "nodeId", event.NodeId())

	// 노드 ID를 통해 노드 찾기
	node, exists := s.nodes[event.NodeId()]
	if !exists {
		event.ResponseChan <- media.NewErrorResponse("Node not found")
		return
	}

	sink := node.(media.MediaSink)

	// 스트림 존재 확인 (Subscribe는 기존 스트림에만 가능)
	stream, exists := s.streams[event.StreamId]
	if !exists {
		event.ResponseChan <- media.NewErrorResponse("Stream not found")
		return
	}

	// 코덱 호환성 검증: 스트림의 모든 트랙 코덱이 Sink에서 지원되는지 확인
	for _, trackCodec := range stream.GetTrackCodecs() {
		if !media.ContainsCodec(event.SupportedCodecs, trackCodec) {
			event.ResponseChan <- media.NewErrorResponse(fmt.Sprintf("Unsupported codec: %d", trackCodec))
			return
		}
	}

	// 성공 응답 먼저 전송 (AddSink에서 캐시 데이터 전송 시 데드락 방지)
	event.ResponseChan <- media.NewSuccessResponse()

	// Sink를 스트림에 추가 (캐시된 데이터 자동 전송 포함)
	stream.AddSink(sink)

	slog.Info("Sink registered for subscribe", "streamId", event.StreamId, "sinkId", event.NodeId(), "nodeType", event.NodeType.String())
}

// handleSubscribeStopped 재생 중지 이벤트 처리
func (s *MediaServer) handleSubscribeStopped(event media.SubscribeStopped) {
	slog.Info("Subscribe stopped", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())

	// 노드 찾기
	node, exists := s.nodes[event.NodeId()]
	if !exists {
		slog.Debug("Subscribe stopped event for already removed node", "nodeId", event.NodeId())
		return
	}

	sink := node.(media.MediaSink)
	if stream, streamExists := s.streams[event.StreamId]; streamExists {
		stream.RemoveSink(sink)
		slog.Info("Sink removed from stream due to subscribe stop", "nodeId", event.NodeId(), "streamId", event.StreamId)
	} else {
		slog.Warn("Stream not found for subscribe stop", "streamId", event.StreamId, "nodeId", event.NodeId())
	}

	// 노드 제거
	s.RemoveNode(event.NodeId())
}
