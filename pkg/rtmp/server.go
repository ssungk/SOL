package rtmp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
)

type Server struct {
	sessions      map[*session]struct{}
	port          int
	channel       chan interface{}
	streamManager *StreamManager // 스트림 관리자
}

func NewServer() *Server {
	rtmp := &Server{
		sessions:      make(map[*session]struct{}),
		port:          1935,
		channel:       make(chan interface{}, 100), // 이벤트 채널 초기화
		streamManager: NewStreamManager(),           // 스트림 매니저 초기화
	}
	return rtmp
}

func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}

	// 이벤트 루프 시작
	go s.eventLoop()
	
	// 연결 수락 시작
	go s.acceptConnections(ln)

	return nil
}

func (s *Server) Stop() {

}

func (s *Server) eventLoop() {
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		}
	}
}

func (s *Server) channelHandler(data interface{}) {
	switch v := data.(type) {
	case Terminated:
		s.TerminatedEventHandler(v.Id)
	case ConnectionEstablished:
		slog.Info("New connection established", "sessionId", v.SessionId, "remoteAddr", v.RemoteAddr)
	case ConnectionClosed:
		slog.Info("Connection closed", "sessionId", v.SessionId, "reason", v.Reason)
	case PublishStarted:
		slog.Info("Publish started", "sessionId", v.SessionId, "streamName", v.StreamName, "streamId", v.StreamId)
		s.handlePublishStarted(v)
	case PublishStopped:
		slog.Info("Publish stopped", "sessionId", v.SessionId, "streamName", v.StreamName, "streamId", v.StreamId)
		s.handlePublishStopped(v)
	case PlayStarted:
		slog.Info("Play started", "sessionId", v.SessionId, "streamName", v.StreamName, "streamId", v.StreamId)
		s.handlePlayStarted(v)
	case PlayStopped:
		slog.Info("Play stopped", "sessionId", v.SessionId, "streamName", v.StreamName, "streamId", v.StreamId)
		s.handlePlayStopped(v)
	case StreamCreated:
		slog.Info("Stream created", "sessionId", v.SessionId, "streamId", v.StreamId)
	case AudioData:
		slog.Debug("Audio data received", "sessionId", v.SessionId, "streamName", v.StreamName, "timestamp", v.Timestamp, "dataSize", len(v.Data))
		s.handleAudioData(v)
	case VideoData:
		slog.Debug("Video data received", "sessionId", v.SessionId, "streamName", v.StreamName, "timestamp", v.Timestamp, "frameType", v.FrameType, "dataSize", len(v.Data))
		s.handleVideoData(v)
	case MetaData:
		slog.Info("Metadata received", "sessionId", v.SessionId, "streamName", v.StreamName, "metadata", v.Metadata)
		s.handleMetaData(v)
	case ErrorOccurred:
		slog.Error("Error occurred", "sessionId", v.SessionId, "context", v.Context, "error", v.Error)
	case FCUnpublishReceived:
		slog.Info("FCUnpublish received", "sessionId", v.SessionId, "streamName", v.StreamName, "streamId", v.StreamId)
		// FCUnpublish는 보통 publish 종료를 예고하는 명령어이므로 별도 처리가 필요할 수 있음
	default:
		slog.Warn("Unknown event type", "eventType", fmt.Sprintf("%T", v))
	}
}

func (s *Server) TerminatedEventHandler(id string) {

}

// Publish 시작 처리
func (s *Server) handlePublishStarted(event PublishStarted) {
	// 세션 찾기
	publisher := s.findSessionById(event.SessionId)
	if publisher == nil {
		slog.Error("Publisher session not found", "sessionId", event.SessionId)
		return
	}

	// 스트림 생성 또는 가져오기
	stream := s.streamManager.GetOrCreateStream(event.StreamName)
	stream.SetPublisher(publisher)

	slog.Info("Publisher registered", "streamName", event.StreamName, "sessionId", event.SessionId)
}

// Publish 종료 처리
func (s *Server) handlePublishStopped(event PublishStopped) {
	stream := s.streamManager.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	stream.RemovePublisher()
	slog.Info("Publisher unregistered", "streamName", event.StreamName, "sessionId", event.SessionId)

	// 스트림이 비활성 상태면 제거
	if !stream.IsActive() {
		s.streamManager.RemoveStream(event.StreamName)
	}
}

// Play 시작 처리
func (s *Server) handlePlayStarted(event PlayStarted) {
	// 세션 찾기
	player := s.findSessionById(event.SessionId)
	if player == nil {
		slog.Error("Player session not found", "sessionId", event.SessionId)
		return
	}

	// 스트림 생성 또는 가져오기
	stream := s.streamManager.GetOrCreateStream(event.StreamName)
	stream.AddPlayer(player)

	slog.Info("Player registered", "streamName", event.StreamName, "sessionId", event.SessionId, "playerCount", stream.GetPlayerCount())

	// 캐시된 데이터 전송 (메타데이터 + GOP)
	s.sendCachedDataToPlayer(player, stream)
}

// Play 종료 처리
func (s *Server) handlePlayStopped(event PlayStopped) {
	stream := s.streamManager.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	player := s.findSessionById(event.SessionId)
	if player == nil {
		return
	}

	stream.RemovePlayer(player)
	slog.Info("Player unregistered", "streamName", event.StreamName, "sessionId", event.SessionId, "playerCount", stream.GetPlayerCount())

	// 스트림이 비활성 상태면 제거
	if !stream.IsActive() {
		s.streamManager.RemoveStream(event.StreamName)
	}
}

// 오디오 데이터 처리
func (s *Server) handleAudioData(event AudioData) {
	stream := s.streamManager.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	// 모든 플레이어에게 전송
	stream.BroadcastToPlayers(func(player *session) {
		s.sendAudioToPlayer(player, event)
	})
}

// 비디오 데이터 처리
func (s *Server) handleVideoData(event VideoData) {
	stream := s.streamManager.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	// GOP 캐시 업데이트
	stream.AddVideoFrame(event.FrameType, event.Timestamp, event.Data)

	// 모든 플래이어에게 전송
	stream.BroadcastToPlayers(func(player *session) {
		s.sendVideoToPlayer(player, event)
	})
}

// 메타데이터 처리
func (s *Server) handleMetaData(event MetaData) {
	stream := s.streamManager.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	// 메타데이터 캐시
	stream.SetMetadata(event.Metadata)

	// 모든 플레이어에게 전송
	stream.BroadcastToPlayers(func(player *session) {
		s.sendMetaDataToPlayer(player, event)
	})
}

// 세션 ID로 세션 찾기
func (s *Server) findSessionById(sessionId string) *session {
	for session := range s.sessions {
		if session.sessionId == sessionId {
			return session
		}
	}
	return nil
}

// 플레이어에게 캐시된 데이터 전송
func (s *Server) sendCachedDataToPlayer(player *session, stream *Stream) {
	// publisher가 없으면 캐시된 데이터만 전송
	publisherSessionId := "unknown"
	publisher := stream.GetPublisher()
	if publisher != nil {
		publisherSessionId = publisher.sessionId
	}

	// 1. 메타데이터 전송
	metadata := stream.GetMetadata()
	if metadata != nil {
		go s.sendMetaDataToPlayer(player, MetaData{
			SessionId:  publisherSessionId,
			StreamName: stream.GetName(),
			Metadata:   metadata,
		})
	}

	// 2. GOP 캐시 전송
	gopCache := stream.GetGOPCache()
	for _, frame := range gopCache {
		if frame.msgType == 8 { // audio
			go s.sendAudioToPlayer(player, AudioData{
				SessionId:  publisherSessionId,
				StreamName: stream.GetName(),
				Timestamp:  frame.timestamp,
				Data:       frame.data,
			})
		} else if frame.msgType == 9 { // video
			go s.sendVideoToPlayer(player, VideoData{
				SessionId:  publisherSessionId,
				StreamName: stream.GetName(),
				Timestamp:  frame.timestamp,
				FrameType:  frame.frameType,
				Data:       frame.data,
			})
		}
	}
}

// 플레이어에게 오디오 데이터 전송
func (s *Server) sendAudioToPlayer(player *session, event AudioData) {
	err := player.writer.writeAudioData(player.conn, event.Data, event.Timestamp)
	if err != nil {
		slog.Error("Failed to send audio to player", "sessionId", player.sessionId, "err", err)
	}
}

// 플레이어에게 비디오 데이터 전송
func (s *Server) sendVideoToPlayer(player *session, event VideoData) {
	err := player.writer.writeVideoData(player.conn, event.Data, event.Timestamp)
	if err != nil {
		slog.Error("Failed to send video to player", "sessionId", player.sessionId, "err", err)
	}
}

// 플레이어에게 메타데이터 전송
func (s *Server) sendMetaDataToPlayer(player *session, event MetaData) {
	err := player.writer.writeScriptData(player.conn, "onMetaData", event.Metadata)
	if err != nil {
		slog.Error("Failed to send metadata to player", "sessionId", player.sessionId, "err", err)
	}
}

func (s *Server) createListener() (net.Listener, error) {
	ln, err := net.Listen("tcp", ":1935")
	if err != nil {
		slog.Info("Error starting RTMP server", "err", err)
		return nil, err
	}

	return ln, nil
}

func (s *Server) acceptConnections(ln net.Listener) {
	defer closeWithLog(ln)
	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Error("Accept failed", "err", err)
			// TODO: 종료 로직 필요
			return
		}
		
		// 세션 생성 시 서버의 이벤트 채널을 전달
		session := s.newSessionWithChannel(conn)
		s.sessions[session] = struct{}{}
	}
}

// 채널을 연결한 세션 생성
func (s *Server) newSessionWithChannel(conn net.Conn) *session {
	// UUID 생성
	sessionId := s.generateSessionId()
	
	session := &session{
		reader:          newMessageReader(),
		writer:          newMessageWriter(),
		conn:            conn,
		externalChannel: s.channel, // 서버의 이벤트 채널 연결
		messageChannel:  make(chan *Message, 10),
		sessionId:       sessionId,
	}

	go session.handleRead()
	go session.handleEvent()

	return session
}

// 세션 ID 생성 (간단한 UUID 형태)
func (s *Server) generateSessionId() string {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "unknown"
	}
	return hex.EncodeToString(bytes)[:8] // 8자리로 단축
}

func closeWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "err", err)
	}
}
