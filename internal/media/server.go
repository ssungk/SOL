package media

import (
	"context"
	"fmt"
	"log/slog"
	"sol/pkg/media"
	"sol/pkg/rtmp"
	"sol/pkg/rtsp"
	"sync"
)

// represents stream configuration
type StreamConfig struct {
	GopCacheSize        int
	MaxPlayersPerStream int
}

type MediaServer struct {
	rtmp     *rtmp.Server
	rtsp     *rtsp.Server
	channel  chan interface{}
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // 송신자들을 추적하기 위한 WaitGroup
	
	// 통합 스트림 및 노드 관리
	streams map[string]*media.Stream     // streamId -> Stream
	nodes   map[uintptr]media.MediaNode  // nodeId -> MediaNode (Source|Sink)
}

func NewMediaServer(rtmpPort, rtspPort, rtspTimeout int, streamConfig StreamConfig) *MediaServer {
	// 자체적으로 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	mediaServer := &MediaServer{
		channel: make(chan interface{}, 10),
		ctx:     ctx,
		cancel:  cancel,
		streams: make(map[string]*media.Stream),
		nodes:   make(map[uintptr]media.MediaNode),
	}

	// RTMP 서버 생성 시 MediaServer의 채널과 WaitGroup 전달
	mediaServer.rtmp = rtmp.NewServer(rtmpPort, rtmp.RTMPStreamConfig{
		GopCacheSize:        streamConfig.GopCacheSize,
		MaxPlayersPerStream: streamConfig.MaxPlayersPerStream,
	}, mediaServer.channel, &mediaServer.wg)


	// RTSP 서버 생성 시 MediaServer의 채널과 WaitGroup 전달
	mediaServer.rtsp = rtsp.NewServer(rtsp.RTSPConfig{
		Port:    rtspPort,
		Timeout: rtspTimeout,
	}, mediaServer.channel, &mediaServer.wg)
	
	return mediaServer
}

func (s *MediaServer) Start() error {
	slog.Info("Media servers starting...")

	// RTMP 서버 시작
	if err := s.rtmp.Start(); err != nil {
		slog.Error("Failed to start RTMP server", "err", err)
		return err
	}
	slog.Info("RTMP Server started")


	// RTSP 서버 시작
	if err := s.rtsp.Start(); err != nil {
		slog.Error("Failed to start RTSP server", "err", err)
		return err
	}
	slog.Info("RTSP Server started")

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
		case data := <-s.channel:
			s.channelHandler(data)
		case <-s.ctx.Done():
			s.shutdown()
			return
		}
	}
}

func (s *MediaServer) channelHandler(data interface{}) {
	switch v := data.(type) {
	// 공통 이벤트 처리
	case media.PublishStarted:
		s.handlePublishStarted(v)
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
	default:
		slog.Warn("Unknown event type", "eventType", fmt.Sprintf("%T", v))
	}
}

// shutdown performs the actual shutdown sequence
func (s *MediaServer) shutdown() {
	slog.Info("Media event loop stopping...")

	s.rtmp.Stop()
	slog.Info("RTMP Server stopped")


	s.rtsp.Stop()
	slog.Info("RTSP Server stopped")

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
		slog.Info("Registered source", "streamId", streamId, "nodeId", nodeId, "nodeType", node.MediaType())
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
		
		slog.Info("Registered sink", "streamId", streamId, "nodeId", nodeId, "nodeType", node.MediaType())
	}
}

// 노드 제거
func (s *MediaServer) RemoveNode(nodeId uintptr) {
	node, exists := s.nodes[nodeId]
	if !exists {
		return
	}
	
	// 노드를 포함하는 스트림 찾기
	var targetStreamId string
	for streamId, stream := range s.streams {
		// Sink인 경우
		if _, ok := node.(media.MediaSink); ok {
			for _, sink := range stream.GetSinks() {
				if sink.ID() == nodeId {
					stream.RemoveSink(sink)
					targetStreamId = streamId
					break
				}
			}
		}
		
		if targetStreamId != "" {
			break
		}
	}
	
	// 노드 제거
	delete(s.nodes, nodeId)
	
	slog.Info("Removed node", "nodeId", nodeId, "streamId", targetStreamId, "nodeType", node.MediaType())
	
	// 스트림에 더 이상 sink가 없으면 스트림 정리
	if targetStreamId != "" {
		if stream := s.streams[targetStreamId]; stream != nil && stream.GetSinkCount() == 0 {
			stream.Stop()
			delete(s.streams, targetStreamId)
			slog.Info("Removed empty stream", "streamId", targetStreamId)
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
func (s *MediaServer) GetStreamStats() map[string]interface{} {
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
	
	return map[string]interface{}{
		"total_streams": len(s.streams),
		"total_nodes":   len(s.nodes),
		"source_count":  sourceCount,
		"sink_count":    sinkCount,
	}
}

// 공통 이벤트 핸들러들

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
	if event.NodeType == media.MediaNodeTypeRTSP {
		if rtspSession, ok := node.(*rtsp.Session); ok {
			streamId = rtspSession.GetStreamPath()
			stream = s.GetOrCreateStream(streamId)
			rtspSession.SetStream(stream)
			slog.Info("Stream created and connected for RTSP publish", "streamId", streamId, "sessionId", rtspSession.GetStreamPath())
		}
	} else if event.Stream != nil {
		// RTMP 등 다른 프로토콜의 경우 이미 생성된 스트림 사용
		stream = event.Stream
		streamId = stream.GetId()
	} else {
		slog.Error("No stream provided and unable to create for node type", "nodeType", event.NodeType.String())
		return
	}
	
	// 스트림을 MediaServer에 등록
	s.streams[streamId] = stream
	
	slog.Info("Source registered for publish", "streamId", streamId, "sourceId", event.NodeId(), "nodeType", event.NodeType.String())
}

// handlePublishStopped 발행 중지 이벤트 처리  
func (s *MediaServer) handlePublishStopped(event media.PublishStopped) {
	slog.Info("Publish stopped", "nodeId", event.NodeId(), "streamId", event.StreamId, "nodeType", event.NodeType.String())
	
	// 노드 제거는 이미 RegisterNode/RemoveNode에서 처리됨
	slog.Info("Source unregistered from publish", "streamId", event.StreamId, "sourceId", event.NodeId())
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
	if event.NodeType == media.MediaNodeTypeRTSP {
		if rtspSession, ok := sink.(*rtsp.Session); ok {
			rtspSession.SetStream(stream)
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
	
	// 노드 제거는 이미 RegisterNode/RemoveNode에서 처리됨
	slog.Info("Sink unregistered from play", "streamId", event.StreamId, "sinkId", event.NodeId())
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
