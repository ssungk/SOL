package srt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	gosrt "github.com/datarhei/gosrt"
)

// Server SRT 서버 구조체
type Server struct {
	port               int
	timeout            int
	mediaServerChannel chan<- any      // MediaServer로 이벤트 전송
	wg                 *sync.WaitGroup // 외부 WaitGroup 참조
	listener           gosrt.Listener  // gosrt 리스너
	ctx                context.Context
	cancel             context.CancelFunc

	// 세션 관리
	sessions      map[uintptr]*Session // sessionID -> Session
	sessionsMutex sync.RWMutex
}

// NewServer 새로운 SRT 서버 생성
func NewServer(config SRTConfig, externalChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		port:               config.Port,
		timeout:            config.Timeout,
		mediaServerChannel: externalChannel, // MediaServer로 이벤트 전송
		wg:                 wg,              // 외부 WaitGroup 참조
		ctx:                ctx,
		cancel:             cancel,
		sessions:           make(map[uintptr]*Session),
	}
}

// Start 서버 시작 (Server 인터페이스 구현)
func (s *Server) Start() error {
	err := s.setupListener()
	if err != nil {
		return err
	}

	// 연결 수락 시작
	go s.acceptConnections()
	go s.eventLoop()

	slog.Info("SRT server started", "port", s.port)
	return nil
}

// Stop 서버 중지 (Server 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("SRT Server stopping...")

	// Cancel context
	s.cancel()

	// Close listener
	if s.listener != nil {
		s.listener.Close()
		slog.Info("SRT Listener closed")
	}

	slog.Info("SRT Server stopped successfully")
}

// ID 서버 ID 반환 (Server 인터페이스 구현)
func (s *Server) ID() string {
	return "srt"
}

// setupListener SRT 리스너 설정
func (s *Server) setupListener() error {
	if s.listener != nil {
		return fmt.Errorf("server already started")
	}

	// gosrt.DefaultConfig() 사용하여 기본 설정으로 시작
	config := gosrt.DefaultConfig()
	config.TransmissionType = "live"
	
	addr := fmt.Sprintf(":%d", s.port)
	listener, err := gosrt.Listen("srt", addr, config)
	if err != nil {
		slog.Error("Error starting SRT server", "err", err)
		return err
	}

	s.listener = listener
	return nil
}

// eventLoop 이벤트 루프
func (s *Server) eventLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer s.shutdown()
	for {
		select {
		case <-s.ctx.Done():
			return
		}
	}
}

// shutdown 종료 처리
func (s *Server) shutdown() {
	if s.listener != nil {
		s.listener.Close()
	}
	slog.Info("SRT server shutdown completed")
}

// acceptConnections 연결 수락
func (s *Server) acceptConnections() {
	defer s.cancel()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// 연결 요청 수락
			req, err := s.listener.Accept2()
			if err != nil {
				select {
				case <-s.ctx.Done():
					return // 서버가 종료 중이면 정상 종료
				default:
					slog.Error("SRT accept failed", "err", err)
					continue
				}
			}

			// 연결 요청 처리
			go s.handleConnectionRequest(req)
		}
	}
}

// handleConnectionRequest 연결 요청 처리
func (s *Server) handleConnectionRequest(req gosrt.ConnRequest) {
	// 연결 수락
	conn, err := req.Accept()
	if err != nil {
		slog.Error("Failed to accept SRT connection", "err", err)
		return
	}

	// 새 세션 생성
	session := NewSession(conn, s.mediaServerChannel, s.wg)
	sessionID := session.ID()

	s.sessionsMutex.Lock()
	s.sessions[sessionID] = session
	s.sessionsMutex.Unlock()


	// 세션 시작
	go func() {
		defer func() {
			// 세션 정리
			s.sessionsMutex.Lock()
			delete(s.sessions, sessionID)
			s.sessionsMutex.Unlock()

			if err := session.Close(); err != nil {
				slog.Error("Error closing SRT session", "sessionId", sessionID, "err", err)
			}
		}()

		// 세션 실행
		if err := session.Run(); err != nil {
			slog.Error("SRT session error", "sessionId", sessionID, "err", err)
		}
	}()
}