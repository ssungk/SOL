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
	// 서버 설정
	config SRTConfig

	// MediaServer와 통신
	mediaServerChannel chan<- any // MediaServer로 이벤트 전송

	// 동기화
	listener gosrt.Listener     // 리스너 참조 저장
	ctx      context.Context    // 컨텍스트
	cancel   context.CancelFunc // 컨텍스트 취소 함수
	wg       *sync.WaitGroup
}

// NewServer 새로운 SRT 서버 생성
func NewServer(config SRTConfig, mediaServerChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		config:             config,
		mediaServerChannel: mediaServerChannel,
		ctx:                ctx,
		cancel:             cancel,
		wg:                 wg,
	}
}

// Start 서버 시작 (Server 인터페이스 구현)
func (s *Server) Start() error {
	err := s.setupListener()
	if err != nil {
		return err
	}

	// 연결 수락 및 이벤트 루프 시작
	go s.acceptConnections()
	go s.eventLoop()

	slog.Info("SRT server started", "port", s.config.Port)
	return nil
}

// Stop 서버 중지 (Server 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("SRT Server stopping...")
	s.cancel()
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

	// gosrt 설정
	config := gosrt.DefaultConfig()
	config.TransmissionType = "live"

	addr := fmt.Sprintf(":%d", s.config.Port)
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
	// SRT 리스너 종료 (gosrt.Listener.Close()는 에러를 반환하지 않음)
	if s.listener != nil {
		s.listener.Close()
		slog.Debug("SRT listener closed")
	}
	slog.Info("SRT server shutdown completed")
}

// acceptConnections 연결 수락
func (s *Server) acceptConnections() {
	defer s.cancel()

	for {
		// 연결 요청 수락 (Accept2 방식)
		req, err := s.listener.Accept2()
		if err != nil {
			slog.Error("SRT accept failed", "err", err)
			return
		}

		// TODO: 스트림 ID 체크 등 추가 필요
		conn, err := req.Accept()
		if err != nil {
			slog.Error("Failed to accept SRT connection", "err", err)
			continue // 이 연결만 건너뛰고 다음 연결 대기
		}

		// 새 세션 생성 (RTMP와 동일)
		session := NewSession(conn, s.mediaServerChannel, s.wg)
		slog.Info("New SRT session created", "sessionId", session.ID(), "remoteAddr", conn.RemoteAddr())
	}
}
