package rtsp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sol/pkg/media"
	"sol/pkg/rtp"
	"sol/pkg/utils"
	"sync"
)

// Server represents an RTSP server
type Server struct {
	port            int
	timeout         int
	sessions        map[string]*Session // sessionId -> session
	rtpTransport    *rtp.RTPTransport
	rtpStarted      bool
	channel         chan any    // 내부 채널
	externalChannel chan<- any // 외부 송신 전용 채널
	wg              *sync.WaitGroup     // 외부 WaitGroup 참조
	listener        net.Listener
	ctx             context.Context
	cancel          context.CancelFunc
	running         bool              // 서버 실행 상태
}

// NewServer creates a new RTSP server
func NewServer(config RTSPConfig, externalChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Server{
		port:          config.Port,
		timeout:       config.Timeout,
		sessions:      make(map[string]*Session),
		rtpTransport:  rtp.NewRTPTransport(),
		channel:       make(chan any, media.DefaultChannelBufferSize), // 내부 채널
		externalChannel: externalChannel,             // 외부 송신 전용 채널
		wg:            wg,                           // 외부 WaitGroup 참조
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start starts the RTSP server
// Start 서버 시작 (ProtocolServer 인터페이스 구현)
func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}
	s.listener = ln
	s.running = true
	
	// RTP transport는 첫 번째 SETUP 요청시에 시작됩니다
	
	// Start event loop
	go s.eventLoop()
	
	// Start accepting connections
	go s.acceptConnections(ln)
	
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("RTSP Server stopping...")
	s.running = false
	
	// Cancel context
	s.cancel()
	
	// Stop RTP transport
	if s.rtpStarted {
		s.rtpTransport.Stop()
	}
	
	// Close listener
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Error("Error closing RTSP listener", "err", err)
		} else {
			slog.Info("RTSP Listener closed")
		}
	}
	
	// Close all sessions
	slog.Info("Closing all RTSP sessions", "sessionCount", len(s.sessions))
	for sessionId, session := range s.sessions {
		session.Stop()
		slog.Debug("RTSP session stopped", "sessionId", sessionId)
	}
	
	// Clear data structures
	s.sessions = make(map[string]*Session)
	
	// Clean up channel
	for {
		select {
		case <-s.channel:
			// Drain remaining events
		default:
			goto cleanup_done
		}
	}
	
cleanup_done:
	close(s.channel)
	slog.Info("RTSP Server stopped successfully")
}

// Name 서버 이름 반환 (ProtocolServer 인터페이스 구현)
func (s *Server) Name() string {
	return "rtsp"
}



// eventLoop processes events
func (s *Server) eventLoop() {
	// 송신자로 등록
	s.wg.Add(1)
	defer s.wg.Done()
	
	for {
		select {
		case event := <-s.channel:
			s.handleEvent(event)
		case <-s.ctx.Done():
			slog.Info("RTSP Event loop stopping...")
			return
		}
	}
}

// handleEvent handles different types of events and forwards to MediaServer
func (s *Server) handleEvent(event any) {
	// 모든 이벤트를 MediaServer로 전달
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- event:
			slog.Debug("Event forwarded to MediaServer", "eventType", utils.TypeName(event))
		default:
			slog.Warn("Failed to forward event to MediaServer (channel full)", "eventType", utils.TypeName(event))
		}
	}
}

// 세션 종료 시 세션 맵에서 제거 (MediaServer에서 나머지 처리)
func (s *Server) removeSession(sessionId string) {
	delete(s.sessions, sessionId)
	slog.Info("RTSP session removed from server", "sessionId", sessionId)
}

// createListener creates a TCP listener
func (s *Server) createListener() (net.Listener, error) {
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("Error starting RTSP server", "err", err)
		return nil, err
	}
	
	return ln, nil
}

// acceptConnections accepts incoming connections
func (s *Server) acceptConnections(ln net.Listener) {
	defer utils.CloseWithLog(ln)
	
	for {
		// Check for context cancellation
		select {
		case <-s.ctx.Done():
			slog.Info("RTSP accept loop stopping...")
			return
		default:
		}
		
		conn, err := ln.Accept()
		if err != nil {
			// Check if listener was closed
			select {
			case <-s.ctx.Done():
				slog.Info("RTSP accept loop stopped (listener closed)")
				return
			default:
				slog.Error("RTSP accept failed", "err", err)
				return
			}
		}
		
		// Create new session with MediaServer channel for direct event forwarding
		session := NewSession(conn, s.externalChannel, s.rtpTransport)
		s.sessions[session.sessionId] = session
		
		// Start session handling
		session.Start()
		
		slog.Info("New RTSP session created", "sessionId", session.sessionId, "remoteAddr", conn.RemoteAddr())
	}
}


// ensureRTPTransport starts RTP transport if not already started
func (s *Server) ensureRTPTransport() error {
	if s.rtpStarted {
		return nil
	}
	
	// Start RTP transport (use base port + 1000 for RTP port)
	rtpPort := s.port + 1000
	if err := s.rtpTransport.StartUDP(rtpPort); err != nil {
		slog.Error("Failed to start RTP transport", "err", err)
		return err
	}
	
	s.rtpStarted = true
	slog.Info("RTP transport started on demand", "rtpPort", rtpPort)
	return nil
}
