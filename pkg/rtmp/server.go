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

	wg *sync.WaitGroup
}

// 새로운 RTMP 서버 생성
func NewServer(port int, mediaServerChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		port:               port,
		mediaServerChannel: mediaServerChannel,
		listener:           nil,
		ctx:                ctx,
		cancel:             cancel,
		wg:                 wg,
	}

	return server
}

// setupListener 리스너 설정 (중복 시작 체크 포함)
func (s *Server) setupListener() error {
	if s.listener != nil {
		return fmt.Errorf("server already started")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		slog.Error("Error starting RTMP server", "err", err)
		return err
	}
	s.listener = ln
	return nil
}

// Start 서버 시작 (ProtocolServer 인터페이스 구현)
func (s *Server) Start() error {
	err := s.setupListener()
	if err != nil {
		return err
	}

	// 연결 수락 시작
	go s.acceptConnections()
	go s.eventLoop()

	slog.Info("RTMP server started", "port", s.port)
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("RTMP server stopping...")
	s.cancel()
}

// ID 서버 ID 반환 (ServerInterface 인터페이스 구현)
func (s *Server) ID() string {
	return "rtmp"
}

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

func (s *Server) shutdown() {
	utils.CloseWithLog(s.listener)
	slog.Info("RTMP server shutdown completed")
}

// acceptConnections 연결 수락
func (s *Server) acceptConnections() {
	defer s.cancel()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			slog.Error("RTMP accept failed", "err", err)
			return
		}

		session := newSession(conn, s.mediaServerChannel, s.wg)
		slog.Info("New RTMP session created", "sessionId", session.ID(), "remoteAddr", conn.RemoteAddr())
	}
}
