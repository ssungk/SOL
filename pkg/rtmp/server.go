package rtmp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
)

// StreamConfig는 스트림 설정을 담는 구조체
type StreamConfig struct {
	GopCacheSize        int
	MaxPlayersPerStream int
}

type Server struct {
	sessions map[string]*session // sessionId를 키로 사용
	streams  map[string]*Stream  // 스트림 직접 관리
	port     int
	channel  chan interface{}    // 내부 채널
	externalChannel chan<- interface{} // 외부 송신 전용 채널
	wg       *sync.WaitGroup     // 외부 WaitGroup 참조
	listener net.Listener        // 리스너 참조 저장
	ctx      context.Context     // 컨텍스트
	cancel   context.CancelFunc  // 컨텍스트 취소 함수
	streamConfig StreamConfig     // 스트림 설정
}

func NewServer(port int, streamConfig StreamConfig, externalChannel chan<- interface{}, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	server := &Server{
		sessions: make(map[string]*session), // sessionId를 키로 사용
		streams:  make(map[string]*Stream),  // 스트림 맵 초기화
		port:     port,
		channel:  make(chan interface{}, 100), // 내부 채널
		externalChannel: externalChannel,         // 외부 송신 전용 채널
		wg:       wg,                           // 외부 WaitGroup 참조
		ctx:      ctx,
		cancel:   cancel,
		streamConfig: streamConfig,
	}
	return server
}

func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}
	s.listener = ln // 리스너 참조 저장

	// 이벤트 루프 시작
	go s.eventLoop()

	// 연결 수락 시작
	go s.acceptConnections(ln)

	return nil
}

func (s *Server) Stop() {
	slog.Info("Server stopping...")

	// 1. 컨텍스트 취소 (모든 고루틴에 종료 신호)
	s.cancel()

	// 2. 새로운 연결 차단 (리스너 종료)
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Error("Error closing listener", "err", err)
		} else {
			slog.Info("Listener closed")
		}
	}

	// 3. 모든 세션 종료
	slog.Info("Closing all sessions", "sessionCount", len(s.sessions))
	for sessionId, session := range s.sessions {
		if session.conn != nil {
			if err := session.conn.Close(); err != nil {
				slog.Error("Error closing session connection", "sessionId", sessionId, "err", err)
			}
		}
	}

	// 4. 모든 스트림 청소
	slog.Info("Clearing all streams", "streamCount", len(s.streams))
	for streamName, stream := range s.streams {
		stream.RemovePublisher() // 캐시 청소
		slog.Debug("Stream cleared", "streamName", streamName)
	}

	// 5. 맵 청소
	s.sessions = make(map[string]*session)
	s.streams = make(map[string]*Stream)

	// 6. 이벤트 채널 청소 (남은 이벤트 처리)
	for {
		select {
		case <-s.channel:
			// 남은 이벤트 버리기
		default:
			// 채널이 비었으면 종료
			goto cleanup_done
		}
	}

cleanup_done:
	close(s.channel)
	slog.Info("Server stopped successfully")
}

func (s *Server) eventLoop() {
	// 송신자로 등록
	s.wg.Add(1)
	defer s.wg.Done()
	
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		case <-s.ctx.Done():
			slog.Info("RTMP event loop stopping...")
			return
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
	stream := s.GetOrCreateStream(event.StreamName, s.streamConfig)
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
	stream := s.GetOrCreateStream(event.StreamName, s.streamConfig)
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
func (s *Server) GetOrCreateStream(streamName string, config StreamConfig) *Stream {
	stream, exists := s.streams[streamName]
	if !exists {
		stream = NewStream(streamName, config.GopCacheSize, config.MaxPlayersPerStream)
		s.streams[streamName] = stream
		slog.Info("Created new stream", "streamName", streamName, "gopCacheSize", config.GopCacheSize, "maxPlayers", config.MaxPlayersPerStream)
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
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Info("Error starting RTMP server", "err", err)
		return nil, err
	}

	return ln, nil
}

func (s *Server) acceptConnections(ln net.Listener) {
	defer closeWithLog(ln)
	for {
		// 컨텍스트 취소 확인
		select {
		case <-s.ctx.Done():
			slog.Info("Accept loop stopping...")
			return
		default:
			// 비블로킹 방식으로 계속 진행
		}

		conn, err := ln.Accept()
		if err != nil {
			// 리스너가 닫혔을 때 정상 종료
			select {
			case <-s.ctx.Done():
				slog.Info("Accept loop stopped (listener closed)")
				return
			default:
				slog.Error("Accept failed", "err", err)
				return
			}
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
