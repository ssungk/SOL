package rtmp

import (
	"fmt"
	"io"
	"log/slog"
	"net"
)

type Server struct {
	sessions map[string]*session // sessionId를 키로 사용
	streams  map[string]*Stream  // 스트림 직접 관리
	port     int
	channel  chan interface{}
}

func NewServer() *Server {
	server := &Server{
		sessions: make(map[string]*session), // sessionId를 키로 사용
		streams:  make(map[string]*Stream),  // 스트림 맵 초기화
		port:     1935,
		channel:  make(chan interface{}, 100),
	}
	return server
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
	case AudioData:
		slog.Debug("Audio data received", "sessionId", v.SessionId, "streamName", v.StreamName, "timestamp", v.Timestamp, "dataSize", len(v.Data))
		s.handleAudioData(v)
	case VideoData:
		slog.Debug("Video data received", "sessionId", v.SessionId, "streamName", v.StreamName, "timestamp", v.Timestamp, "frameType", v.FrameType, "dataSize", len(v.Data))
		s.handleVideoData(v)
	case MetaData:
		slog.Info("Metadata received", "sessionId", v.SessionId, "streamName", v.StreamName, "metadata", v.Metadata)
		s.handleMetaData(v)
	default:
		slog.Warn("Unknown event type", "eventType", fmt.Sprintf("%T", v))
	}
}

func (s *Server) TerminatedEventHandler(id string) {
	// 세션을 직접 찾기 (O(1))
	targetSession, exists := s.sessions[id]
	if !exists {
		slog.Warn("Session not found for termination", "sessionId", id)
		return
	}

	// 모든 스트림에서 해당 세션 정리
	s.cleanupSessionFromAllStreams(targetSession)
	// 세션 맵에서 제거
	delete(s.sessions, id)
	slog.Info("Session terminated", "sessionId", id)
}

// 모든 스트림에서 세션 정리
func (s *Server) cleanupSessionFromAllStreams(session *session) {
	for streamName, stream := range s.streams {
		stream.CleanupSession(session)
		// 스트림이 비활성 상태면 제거
		if !stream.IsActive() {
			delete(s.streams, streamName)
			slog.Info("Removed inactive stream", "streamName", streamName)
		}
	}
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
	stream := s.GetOrCreateStream(event.StreamName)
	stream.SetPublisher(publisher) // session 객체 직접 전달

	slog.Info("Publisher registered", "streamName", event.StreamName, "sessionId", event.SessionId)
}

// Publish 종료 처리
func (s *Server) handlePublishStopped(event PublishStopped) {
	stream := s.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	stream.RemovePublisher()
	slog.Info("Publisher unregistered", "streamName", event.StreamName, "sessionId", event.SessionId)

	// 스트림이 비활성 상태면 제거
	if !stream.IsActive() {
		s.RemoveStream(event.StreamName)
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
	stream := s.GetOrCreateStream(event.StreamName)
	stream.AddPlayer(player) // session 객체 직접 전달 (캐시 데이터 자동 전송)

	slog.Info("Player registered", "streamName", event.StreamName, "sessionId", event.SessionId, "playerCount", stream.GetPlayerCount())
}

// Play 종료 처리
func (s *Server) handlePlayStopped(event PlayStopped) {
	// 세션 찾기
	player := s.findSessionById(event.SessionId)
	if player == nil {
		slog.Error("Player session not found for stop", "sessionId", event.SessionId)
		return
	}

	stream := s.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	stream.RemovePlayer(player) // session 객체 직접 전달
	slog.Info("Player unregistered", "streamName", event.StreamName, "sessionId", event.SessionId, "playerCount", stream.GetPlayerCount())

	// 스트림이 비활성 상태면 제거
	if !stream.IsActive() {
		s.RemoveStream(event.StreamName)
	}
}

// 오디오 데이터 처리
func (s *Server) handleAudioData(event AudioData) {
	stream := s.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	// Stream에서 직접 처리 및 전송
	stream.ProcessAudioData(event)
}

// 비디오 데이터 처리
func (s *Server) handleVideoData(event VideoData) {
	stream := s.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	// Stream에서 직접 처리 및 전송 (GOP 캐시 업데이트 포함)
	stream.ProcessVideoData(event)
}

// 메타데이터 처리
func (s *Server) handleMetaData(event MetaData) {
	stream := s.GetStream(event.StreamName)
	if stream == nil {
		return
	}

	// Stream에서 직접 처리 및 전송 (메타데이터 캐시 포함)
	stream.ProcessMetaData(event)
}

// 세션 ID로 세션 찾기
func (s *Server) findSessionById(sessionId string) *session {
	return s.sessions[sessionId] // nil이 자동으로 반환됨
}

// GetOrCreateStream은 스트림을 가져오거나 생성
func (s *Server) GetOrCreateStream(streamName string) *Stream {
	stream, exists := s.streams[streamName]
	if !exists {
		stream = NewStream(streamName)
		s.streams[streamName] = stream
		slog.Info("Created new stream", "streamName", streamName)
	}
	return stream
}

// GetStream은 스트림을 가져옴 (없으면 nil 반환)
func (s *Server) GetStream(streamName string) *Stream {
	return s.streams[streamName]
}

// RemoveStream은 스트림을 제거
func (s *Server) RemoveStream(streamName string) {
	delete(s.streams, streamName)
	slog.Info("Removed stream", "streamName", streamName)
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

		// sessionId를 키로 사용해서 세션 저장
		s.sessions[session.sessionId] = session
	}
}

// 채널을 연결한 세션 생성
func (s *Server) newSessionWithChannel(conn net.Conn) *session {
	session := &session{
		reader:          newMessageReader(),
		writer:          newMessageWriter(),
		conn:            conn,
		externalChannel: s.channel, // 서버의 이벤트 채널 연결
		messageChannel:  make(chan *Message, 10),
	}

	// 포인터 주소값을 sessionId로 사용
	session.sessionId = fmt.Sprintf("%p", session)

	go session.handleRead()
	go session.handleEvent()

	return session
}

func closeWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "err", err)
	}
}
