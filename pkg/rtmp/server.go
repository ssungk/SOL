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
	channel chan interface{}
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
		channel:            make(chan interface{}, 10),
		port:               port,
		mediaServerChannel: mediaServerChannel,
		ctx:                ctx,
		cancel:             cancel,
		wg:                 wg,
	}

	return server
}

// Start 서버 시작 (ProtocolServer 인터페이스 구현)
func (s *Server) Start() error {
	//s.wg.Add(1)
	//defer s.wg.Done()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		slog.Error("Error starting RTMP server", "err", err)
		return err
	}
	s.listener = ln

	// 연결 수락 시작
	go s.acceptConnections(ln)
	go s.eventLoop()

	slog.Info("RTMP server started", "port", s.port)
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("RTMP server stopping...")

	s.cancel()

	slog.Info("RTMP server stopped successfully")
}

// Name 서버 이름 반환 (ProtocolServer 인터페이스 구현)
func (s *Server) Name() string {
	return "rtmp"
}

func (s *Server) eventLoop() {
	defer utils.CloseWithLog(s.listener)
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		case <-s.ctx.Done():
			s.shutdown()
			return
		}
	}
}

func (s *Server) channelHandler(data interface{}) {
	switch v := data.(type) {
	default:
		slog.Warn("Unknown event type", "eventType", utils.TypeName(v))
	}
}

func (s *Server) shutdown() {

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

		session := newSession(conn, s.mediaServerChannel)
		slog.Info("New RTMP session created", "sessionId", session.sessionId, "remoteAddr", conn.RemoteAddr())
	}
}
