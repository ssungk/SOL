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
	port               int
	timeout            int
	rtpTransport       *rtp.RTPTransport
	rtpStarted         bool
	mediaServerChannel chan<- any      // MediaServer로 이벤트 전송
	wg                 *sync.WaitGroup // 외부 WaitGroup 참조
	listener           net.Listener
	ctx                context.Context
	cancel             context.CancelFunc
}

// NewServer creates a new RTSP server
func NewServer(config RTSPConfig, externalChannel chan<- any, wg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		port:               config.Port,
		timeout:            config.Timeout,
		rtpTransport:       rtp.NewRTPTransport(),
		mediaServerChannel: externalChannel, // MediaServer로 이벤트 전송
		wg:                 wg,              // 외부 WaitGroup 참조
		ctx:                ctx,
		cancel:             cancel,
	}
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

	slog.Info("RTSP server started", "port", s.port)
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("RTSP Server stopping...")

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

	slog.Info("RTSP Server stopped successfully")
}

func (s *Server) eventLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer utils.CloseWithLog(s.listener)
	for {
		select {
		case <-s.ctx.Done():
			s.shutdown()
			return
		}
	}
}

func (s *Server) shutdown() {
	slog.Info("RTSP server shutdown completed")
}

// ID 서버 ID 반환 (Server 인터페이스 구현)
func (s *Server) ID() string {
	return "rtsp"
}

// setupListener 리스너 설정 (중복 시작 체크 포함)
func (s *Server) setupListener() error {
	if s.listener != nil {
		return fmt.Errorf("server already started")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		slog.Error("Error starting RTSP server", "err", err)
		return err
	}
	s.listener = ln
	return nil
}

// acceptConnections 연결 수락
func (s *Server) acceptConnections() {
	defer s.cancel()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			slog.Error("RTSP accept failed", "err", err)
			return
		}

		// Create new session with MediaServer channel for direct event forwarding
		session := NewSession(conn, s.mediaServerChannel, s.rtpTransport)

		// Send NodeCreated event to MediaServer
		if s.mediaServerChannel != nil {
			select {
			case s.mediaServerChannel <- media.NodeCreated{
				ID:   session.ID(),
				Node: session,
			}:
			default:
			}
		}

		// Start session handling
		go session.handleRequests()
		go session.handleTimeout()

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
	return nil
}
