package rtmp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
)

// pkg/media를 활용하는 RTMP2 서버
type Server struct {
	// 서버 설정
	port         int
	streamConfig RTMPStreamConfig

	// MediaServer와 통신
	mediaServerChannel chan<- any // MediaServer로 이벤트 전송

	// 동기화
	listener net.Listener       // 리스너 참조 저장
	ctx      context.Context    // 컨텍스트
	cancel   context.CancelFunc // 컨텍스트 취소 함수
}

// 새로운 RTMP2 서버 생성
func NewServer(port int, streamConfig RTMPStreamConfig, mediaServerChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		port:         port,
		streamConfig: streamConfig,
		mediaServerChannel: mediaServerChannel,
		ctx:          ctx,
		cancel:       cancel,
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

	// 세션 관리는 MediaServer에서 중앙 처리됨

	slog.Info("RTMP2 server stopped successfully")
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

		// 세션 생성 시 MediaServer 채널을 전달
		_ = s.newSessionWithChannel(conn) // session은 독립적으로 동작
	}
}

// newSessionWithChannel 채널을 연결한 세션 생성
func (s *Server) newSessionWithChannel(conn net.Conn) *session {
	session := newSession(conn, s.mediaServerChannel) // MediaServer 채널 직접 전달

	slog.Info("New RTMP2 session created", "sessionId", session.sessionId, "remoteAddr", conn.RemoteAddr())
	return session
}

// closeWithLog 리소스 종료 로깅
func closeWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "err", err)
	}
}
