package sol

import (
	"context"
	"log/slog"
	"sol/pkg/rtmp"
	"sol/pkg/rtsp"
	"time"
)

// StreamConfig represents stream configuration
type StreamConfig struct {
	GopCacheSize        int
	MaxPlayersPerStream int
}

type Server struct {
	ticker  *time.Ticker
	rtmp    *rtmp.Server
	rtsp    *rtsp.Server
	channel chan interface{}
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewServer(rtmpPort, rtspPort, rtspTimeout int, streamConfig StreamConfig, ctx context.Context) *Server {
	// 취소 가능한 컨텍스트 생성
	childCtx, cancel := context.WithCancel(ctx)

	sol := &Server{
		channel: make(chan interface{}, 10),
		rtmp: rtmp.NewServer(rtmpPort, rtmp.StreamConfig{
			GopCacheSize:        streamConfig.GopCacheSize,
			MaxPlayersPerStream: streamConfig.MaxPlayersPerStream,
		}),
		rtsp: rtsp.NewServer(rtsp.RTSPConfig{
			Port:    rtspPort,
			Timeout: rtspTimeout,
		}),
		ticker: time.NewTicker(1000 * time.Second),
		ctx:    childCtx,
		cancel: cancel,
	}
	return sol
}

func (s *Server) Start() error {
	slog.Info("Sol servers starting...")

	// RTMP 서버 시작
	if err := s.rtmp.Start(); err != nil {
		slog.Error("Failed to start RTMP server", "err", err)
		return err
	}

	slog.Info("RTMP Server started")

	// RTSP 서버 시작
	if err := s.rtsp.Start(); err != nil {
		slog.Error("Failed to start RTSP server", "err", err)
		return err
	}

	slog.Info("RTSP Server started")

	// 이벤트 루프 시작
	go s.eventLoop()

	return nil
}

// Stop stops the server
func (s *Server) Stop() {
	slog.Info("Stopping Sol Server...")

	// 1. 컨텍스트 취소 (모든 고루틴에 종료 신호)
	s.cancel()

	// 2. RTMP 서버 종료
	s.rtmp.Stop()

	// 3. RTSP 서버 종료
	s.rtsp.Stop()

	// 4. 티커 종료
	if s.ticker != nil {
		s.ticker.Stop()
		slog.Info("Ticker stopped")
	}

	// 5. 채널 청소
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
	slog.Info("Sol Server stopped successfully")
}

func (s *Server) eventLoop() {
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		case <-s.ticker.C:
			slog.Info("test")
		case <-s.ctx.Done():
			slog.Info("Sol event loop stopping...")
			return
		}
	}
}

func (s *Server) channelHandler(data interface{}) {

}
