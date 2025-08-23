package media

import (
	"context"
	"fmt"
	"log/slog"
	"sol/pkg/media"
	"sol/pkg/rtmp"
	"sol/pkg/rtsp"
	"sol/pkg/srt"
	"sol/pkg/utils"
	"sync"
)

type MediaServer struct {
	servers map[string]media.Server
	channel chan any
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup // 송신자들을 추적하기 위한 WaitGroup

	// 통합 스트림 및 노드 관리
	streams map[string]*media.Stream    // streamID -> Stream
	nodes   map[uintptr]media.MediaNode // nodeID -> MediaNode (Source|Sink)
}

func NewMediaServer(rtmpPort, rtspPort, rtspTimeout, srtPort, srtTimeout int) *MediaServer {
	ctx, cancel := context.WithCancel(context.Background())

	mediaServer := &MediaServer{
		servers: make(map[string]media.Server),
		channel: make(chan any, media.DefaultChannelBufferSize),
		ctx:     ctx,
		cancel:  cancel,
		streams: make(map[string]*media.Stream),
		nodes:   make(map[uintptr]media.MediaNode),
	}

	// 각 서버를 생성하고 맵에 등록
	rtmpServer := rtmp.NewServer(rtmpPort, mediaServer.channel, &mediaServer.wg)
	mediaServer.servers[rtmpServer.ID()] = rtmpServer

	rtspServer := rtsp.NewServer(rtsp.NewRTSPConfig(rtspPort, rtspTimeout), mediaServer.channel, &mediaServer.wg)
	mediaServer.servers[rtspServer.ID()] = rtspServer

	srtServer := srt.NewServer(srt.NewSRTConfig(srtPort, srtTimeout), mediaServer.channel, &mediaServer.wg)
	mediaServer.servers[srtServer.ID()] = srtServer

	return mediaServer
}

func (s *MediaServer) Start() error {
	slog.Info("Media servers starting...")

	// 모든 서버들을 순차적으로 시작
	for id, server := range s.servers {
		if err := server.Start(); err != nil {
			slog.Error("Failed to start server", "serverID", id, "err", err)
			return fmt.Errorf("failed to start %s server: %w", id, err)
		}
		slog.Info("Server started", "serverID", id, "protocol", server.ID())
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
		case data := <-s.channel:
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
	for id, server := range s.servers {
		server.Stop()
		slog.Info("Server stopped", "serverID", id)
	}

	s.wg.Wait()
	slog.Info("Media Server stopped successfully")
}

// addNode 노드 추가 (이벤트 루프 내에서만 호출)
func (s *MediaServer) addNode(nodeID uintptr, node media.MediaNode) {
	s.nodes[nodeID] = node
	slog.Info("Node added", "nodeID", nodeID, "nodeType", node.NodeType(), "totalNodes", len(s.nodes))
}

// removeNode 노드 제거 (이벤트 루프 내에서만 호출)
func (s *MediaServer) removeNode(nodeID uintptr) {
	node, exists := s.nodes[nodeID]
	if !exists {
		slog.Debug("Node not found for removal", "nodeID", nodeID)
		return
	}

	// 외부에서 의도적으로 노드 종료 시 Close 호출하여 정리 작업 보장
	utils.CloseWithLog(node)

	// 노드 제거
	delete(s.nodes, nodeID)
	slog.Info("Node removed", "nodeID", nodeID, "nodeType", node.NodeType(), "remainingNodes", len(s.nodes))
}

// handleNodeCreated 노드 생성 이벤트 처리
func (s *MediaServer) handleNodeCreated(event media.NodeCreated) {
	slog.Info("Node created", "nodeID", event.ID, "nodeType", event.Node.NodeType().String())
	s.addNode(event.ID, event.Node)
}

// handleNodeTerminated 노드 종료 이벤트 처리
func (s *MediaServer) handleNodeTerminated(event media.NodeTerminated) {
	slog.Info("Node terminated", "nodeID", event.ID)
	s.removeNode(event.ID)
}

// handlePublishStarted 실제 publish 시도 처리
func (s *MediaServer) handlePublishStarted(event media.PublishStarted) {
	streamID := event.Stream.ID()
	slog.Debug("Stream publish attempt requested", "streamID", streamID, "nodeID", event.ID)

	if _, exists := s.streams[streamID]; exists {
		event.ResponseChan <- media.NewErrorResponse("Stream ID was taken by another node")
		return
	}

	s.streams[streamID] = event.Stream
	event.ResponseChan <- media.NewSuccessResponse()
	slog.Info("Stream publish successful", "streamID", streamID, "nodeID", event.ID)
}

// handlePublishStopped 발행 중지 이벤트 처리
func (s *MediaServer) handlePublishStopped(event media.PublishStopped) {
	slog.Info("Publish stopped", "nodeID", event.ID, "streamID", event.StreamID)

	if stream, exists := s.streams[event.StreamID]; exists {
		stream.Stop()
		delete(s.streams, event.StreamID)
		slog.Info("Stream removed due to publish stop", "streamID", event.StreamID)
	}
}

// handleSubscribeStarted 재생 시작 이벤트 처리
func (s *MediaServer) handleSubscribeStarted(event media.SubscribeStarted) {
	slog.Debug("Subscribe attempt requested", "streamID", event.StreamID, "nodeID", event.ID)

	// 노드 ID를 통해 노드 찾기
	node, exists := s.nodes[event.ID]
	if !exists {
		event.ResponseChan <- media.NewErrorResponse("Node not found")
		return
	}

	sink := node.(media.MediaSink)

	// 스트림 존재 확인 (Subscribe는 기존 스트림에만 가능)
	stream, exists := s.streams[event.StreamID]
	if !exists {
		event.ResponseChan <- media.NewErrorResponse("Stream not found")
		return
	}

	// 코덱 검증: 스트림의 모든 트랙 코덱이 Sink에서 지원되는지 확인
	for _, trackCodec := range stream.TrackCodecs() {
		if !media.ContainsCodec(event.SupportedCodecs, trackCodec) {
			event.ResponseChan <- media.NewErrorResponse(fmt.Sprintf("Unsupported codec: %d", trackCodec))
			return
		}
	}

	// 성공 응답 먼저 전송 (AddSink에서 캐시 데이터 전송 시 데드락 방지)
	event.ResponseChan <- media.NewSuccessResponse()

	// Sink를 스트림에 추가 (캐시된 데이터 자동 전송 포함)
	stream.AddSink(sink)

	nodeType := sink.NodeType().String()
	slog.Info("Sink registered for subscribe", "streamID", event.StreamID, "sinkId", event.ID, "nodeType", nodeType)
}

// handleSubscribeStopped 재생 중지 이벤트 처리
func (s *MediaServer) handleSubscribeStopped(event media.SubscribeStopped) {
	slog.Info("Subscribe stopped", "nodeID", event.ID, "streamID", event.StreamID)

	// 노드 찾기
	node, exists := s.nodes[event.ID]
	if !exists {
		slog.Debug("Subscribe stopped event for already removed node", "nodeID", event.ID)
		return
	}

	sink := node.(media.MediaSink)
	if stream, streamExists := s.streams[event.StreamID]; streamExists {
		stream.RemoveSink(sink)
		slog.Info("Sink removed from stream due to subscribe stop", "nodeID", event.ID, "streamID", event.StreamID)
	} else {
		slog.Warn("Stream not found for subscribe stop", "streamID", event.StreamID, "nodeID", event.ID)
	}
}
