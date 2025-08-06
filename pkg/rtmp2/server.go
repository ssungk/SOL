package rtmp2

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/media"
	"sync"
)

// StreamManager pkg/media Stream을 관리하는 인터페이스
type StreamManager interface {
	GetOrCreateStream(streamId string) *media.Stream
	GetStream(streamId string) *media.Stream
	RemoveStream(streamId string)
}

// Server pkg/media를 활용하는 RTMP2 서버
type Server struct {
	// 세션 관리
	sessions map[string]*session // sessionId를 키로 사용
	
	// 스트림 매니저 (외부에서 주입)
	streamManager StreamManager
	
	// 서버 설정
	port         int
	streamConfig RTMPStreamConfig
	
	// 이벤트 채널
	channel         chan interface{}   // 내부 채널
	externalChannel chan<- interface{} // 외부 송신 전용 채널
	
	// 동기화
	wg       *sync.WaitGroup  // 외부 WaitGroup 참조
	listener net.Listener     // 리스너 참조 저장
	ctx      context.Context  // 컨텍스트
	cancel   context.CancelFunc // 컨텍스트 취소 함수
}

// NewServer 새로운 RTMP2 서버 생성
func NewServer(port int, streamConfig RTMPStreamConfig, streamManager StreamManager, externalChannel chan<- interface{}, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	server := &Server{
		sessions:        make(map[string]*session),
		streamManager:   streamManager,
		port:           port,
		streamConfig:   streamConfig,
		channel:        make(chan interface{}, 100),
		externalChannel: externalChannel,
		wg:             wg,
		ctx:            ctx,
		cancel:         cancel,
	}
	
	return server
}

// Start 서버 시작
func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}
	s.listener = ln

	// 이벤트 루프 시작
	go s.eventLoop()

	// 연결 수락 시작
	go s.acceptConnections(ln)

	slog.Info("RTMP2 server started", "port", s.port)
	return nil
}

// Stop 서버 중지
func (s *Server) Stop() {
	slog.Info("RTMP2 server stopping...")

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

	// 4. 맵 청소
	s.sessions = make(map[string]*session)

	// 5. 이벤트 채널 청소 (남은 이벤트 처리)
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
	slog.Info("RTMP2 server stopped successfully")
}

// eventLoop 이벤트 루프
func (s *Server) eventLoop() {
	// 송신자로 등록
	s.wg.Add(1)
	defer s.wg.Done()
	
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		case <-s.ctx.Done():
			slog.Info("RTMP2 event loop stopping...")
			return
		}
	}
}

// channelHandler 채널 이벤트 처리
func (s *Server) channelHandler(data interface{}) {
	switch v := data.(type) {
	case Terminated:
		s.handleTerminated(v.Id)
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
	default:
		slog.Warn("Unknown event type", "eventType", fmt.Sprintf("%T", v))
	}
}

// handleTerminated 세션 종료 처리
func (s *Server) handleTerminated(sessionId string) {
	// 세션을 직접 찾기 (O(1))
	targetSession, exists := s.sessions[sessionId]
	if !exists {
		slog.Warn("Session not found for termination", "sessionId", sessionId)
		return
	}

	// 스트림에서 세션 정리
	s.cleanupSessionFromStreams(targetSession)
	
	// 세션 맵에서 제거
	delete(s.sessions, sessionId)
	slog.Info("Session terminated", "sessionId", sessionId)
}

// handlePublishStarted 발행 시작 처리
func (s *Server) handlePublishStarted(event PublishStarted) {
	// 세션 찾기
	session := s.findSessionById(event.SessionId)
	if session == nil {
		slog.Error("Publisher session not found", "sessionId", event.SessionId)
		return
	}

	// RTMP 소스 가져오기
	rtmpSource := session.GetRTMPSource()
	if rtmpSource == nil {
		slog.Error("RTMP source not found in session", "sessionId", event.SessionId)
		return
	}

	// pkg/media 스트림 생성 또는 가져오기
	stream := s.streamManager.GetOrCreateStream(event.StreamName)
	
	// RTMP 소스를 스트림에 연결
	rtmpSource.SetStream(stream)
	
	slog.Info("Publisher registered with media stream", 
		"streamName", event.StreamName, 
		"sessionId", event.SessionId,
		"sourceId", rtmpSource.GetSourceId())
}

// handlePublishStopped 발행 중지 처리
func (s *Server) handlePublishStopped(event PublishStopped) {
	// 세션 찾기
	session := s.findSessionById(event.SessionId)
	if session == nil {
		slog.Error("Publisher session not found for stop", "sessionId", event.SessionId)
		return
	}

	// RTMP 소스 가져오기 및 정리
	rtmpSource := session.GetRTMPSource()
	if rtmpSource != nil {
		rtmpSource.RemoveStream()
		slog.Info("Publisher unregistered from media stream", 
			"streamName", event.StreamName, 
			"sessionId", event.SessionId)
	}

	// 스트림이 더 이상 사용되지 않으면 제거하는 로직은 StreamManager가 처리
}

// handlePlayStarted 재생 시작 처리
func (s *Server) handlePlayStarted(event PlayStarted) {
	// 세션 찾기
	session := s.findSessionById(event.SessionId)
	if session == nil {
		slog.Error("Player session not found", "sessionId", event.SessionId)
		return
	}

	// RTMP 싱크 가져오기
	rtmpSink := session.GetRTMPSink()
	if rtmpSink == nil {
		slog.Error("RTMP sink not found in session", "sessionId", event.SessionId)
		return
	}

	// pkg/media 스트림 가져오기 또는 생성
	stream := s.streamManager.GetOrCreateStream(event.StreamName)
	
	// RTMP 싱크를 스트림에 추가
	if err := stream.AddSink(rtmpSink); err != nil {
		slog.Error("Failed to add sink to stream", 
			"streamName", event.StreamName,
			"sessionId", event.SessionId,
			"err", err)
		return
	}
	
	// 캐시된 데이터를 새 싱크에 전송
	if err := stream.SendCachedDataToSink(rtmpSink); err != nil {
		slog.Error("Failed to send cached data to sink", 
			"streamName", event.StreamName,
			"sessionId", event.SessionId,
			"err", err)
	}

	slog.Info("Player registered with media stream", 
		"streamName", event.StreamName, 
		"sessionId", event.SessionId,
		"sinkId", rtmpSink.GetSinkId(),
		"sinkCount", stream.GetSinkCount())
}

// handlePlayStopped 재생 중지 처리
func (s *Server) handlePlayStopped(event PlayStopped) {
	// 세션 찾기
	session := s.findSessionById(event.SessionId)
	if session == nil {
		slog.Error("Player session not found for stop", "sessionId", event.SessionId)
		return
	}

	// RTMP 싱크 가져오기
	rtmpSink := session.GetRTMPSink()
	if rtmpSink == nil {
		slog.Error("RTMP sink not found for stop", "sessionId", event.SessionId)
		return
	}

	// 스트림에서 싱크 제거
	stream := s.streamManager.GetStream(event.StreamName)
	if stream != nil {
		stream.RemoveSink(rtmpSink)
		slog.Info("Player unregistered from media stream", 
			"streamName", event.StreamName, 
			"sessionId", event.SessionId,
			"sinkCount", stream.GetSinkCount())
	}
}

// findSessionById 세션 ID로 세션 찾기
func (s *Server) findSessionById(sessionId string) *session {
	return s.sessions[sessionId]
}

// cleanupSessionFromStreams 모든 스트림에서 세션 정리
func (s *Server) cleanupSessionFromStreams(session *session) {
	// RTMP 소스 정리
	if rtmpSource := session.GetRTMPSource(); rtmpSource != nil {
		rtmpSource.RemoveStream()
		slog.Debug("Cleaned up RTMP source from session", "sessionId", session.GetID())
	}
	
	// RTMP 싱크 정리  
	if rtmpSink := session.GetRTMPSink(); rtmpSink != nil {
		// 연결된 스트림에서 싱크 제거
		fullStreamPath := session.GetFullStreamPath()
		if fullStreamPath != "" {
			if stream := s.streamManager.GetStream(fullStreamPath); stream != nil {
				stream.RemoveSink(rtmpSink)
			}
		}
		rtmpSink.Stop()
		slog.Debug("Cleaned up RTMP sink from session", "sessionId", session.GetID())
	}
}

// createListener 리스너 생성
func (s *Server) createListener() (net.Listener, error) {
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Info("Error starting RTMP2 server", "err", err)
		return nil, err
	}

	return ln, nil
}

// acceptConnections 연결 수락
func (s *Server) acceptConnections(ln net.Listener) {
	defer closeWithLog(ln)
	for {
		// 컨텍스트 취소 확인
		select {
		case <-s.ctx.Done():
			slog.Info("RTMP2 accept loop stopping...")
			return
		default:
			// 비블로킹 방식으로 계속 진행
		}

		conn, err := ln.Accept()
		if err != nil {
			// 리스너가 닫혔을 때 정상 종료
			select {
			case <-s.ctx.Done():
				slog.Info("RTMP2 accept loop stopped (listener closed)")
				return
			default:
				slog.Error("RTMP2 accept failed", "err", err)
				return
			}
		}

		// 세션 생성 시 서버의 이벤트 채널을 전달
		session := s.newSessionWithChannel(conn)

		// sessionId를 키로 사용해서 세션 저장
		s.sessions[session.sessionId] = session
	}
}

// newSessionWithChannel 채널을 연결한 세션 생성
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

	// 세션 핸들링 고루틴 시작
	go session.handleRead()
	go session.handleEvent()

	slog.Info("New RTMP2 session created", "sessionId", session.sessionId, "remoteAddr", conn.RemoteAddr())
	return session
}

// GetStreamManager 스트림 매니저 반환
func (s *Server) GetStreamManager() StreamManager {
	return s.streamManager
}

// GetSessionCount 세션 개수 반환
func (s *Server) GetSessionCount() int {
	return len(s.sessions)
}

// GetSessions 모든 세션 반환 (복사본)
func (s *Server) GetSessions() map[string]*session {
	sessions := make(map[string]*session)
	for id, session := range s.sessions {
		sessions[id] = session
	}
	return sessions
}

// closeWithLog 리소스 종료 로깅
func closeWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "err", err)
	}
}