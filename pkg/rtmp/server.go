package rtmp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sol/pkg/utils"
	"sync"
)

// pkg/media를 활용하는 RTMP 서버
type Server struct {
	// 서버 설정
	port int

	// MediaServer와 통신
	mediaServerChannel chan<- any // MediaServer로 이벤트 전송

	// 동기화
	listener net.Listener       // 리스너 참조 저장
	ctx      context.Context    // 컨텍스트
	cancel   context.CancelFunc // 컨텍스트 취소 함수
}

// 새로운 RTMP 서버 생성
func NewServer(port int, mediaServerChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		port:               port,
		mediaServerChannel: mediaServerChannel,
		ctx:                ctx,
		cancel:             cancel,
	}

	return server
}

// Start 서버 시작 (ProtocolServer 인터페이스 구현)
func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}
	s.listener = ln

	// 연결 수락 시작
	go s.acceptConnections(ln)

	slog.Info("RTMP server started", "port", s.port)
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("RTMP server stopping...")

	// 1. 컨텍스트 취소 (모든 고루틴에 종료 신호)
	s.cancel()

	// 2. 새로운 연결 차단 (리스너 종료)
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Error("Error closing listener", "err", err)
		} else {
			slog.Info("Listener closed")
		}
		s.listener = nil // 상태 초기화
	}

	// 세션 관리는 MediaServer에서 중앙 처리됨

	slog.Info("RTMP server stopped successfully")
}

// Name 서버 이름 반환 (ProtocolServer 인터페이스 구현)
func (s *Server) Name() string {
	return "rtmp"
}

// createListener 리스너 생성
func (s *Server) createListener() (net.Listener, error) {
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("Error starting RTMP server", "err", err)
		return nil, err
	}

	return ln, nil
}

// acceptConnections 연결 수락
func (s *Server) acceptConnections(ln net.Listener) {
	defer utils.CloseWithLog(ln)
	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Error("RTMP accept failed", "err", err)
			return
		}

		// 세션 생성 시 MediaServer 채널을 전달
		_ = s.newSessionWithChannel(conn) // session은 독립적으로 동작
	}
}

// newSessionWithChannel 채널을 연결한 세션 생성
func (s *Server) newSessionWithChannel(conn net.Conn) *session {
	session := newSession(conn, s.mediaServerChannel) // MediaServer 채널 직접 전달

	slog.Info("New RTMP session created", "sessionId", session.sessionId, "remoteAddr", conn.RemoteAddr())
	return session
}
