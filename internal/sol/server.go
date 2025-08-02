package sol

import (
	"context"
	"fmt"
	"log/slog"
	"sol/pkg/rtmp"
	"sol/pkg/rtsp"
	"sync"
)

// StreamConfig represents stream configuration
type StreamConfig struct {
	GopCacheSize        int
	MaxPlayersPerStream int
}

type Server struct {
	rtmp    *rtmp.Server
	rtsp    *rtsp.Server
	channel chan interface{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup // 송신자들을 추적하기 위한 WaitGroup
}

func NewServer(rtmpPort, rtspPort, rtspTimeout int, streamConfig StreamConfig) *Server {
	// 자체적으로 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	sol := &Server{
		channel: make(chan interface{}, 10),
		ctx:     ctx,
		cancel:  cancel,
	}

	// RTMP 서버 생성 시 Sol 서버의 채널과 WaitGroup 전달
	sol.rtmp = rtmp.NewServer(rtmpPort, rtmp.StreamConfig{
		GopCacheSize:        streamConfig.GopCacheSize,
		MaxPlayersPerStream: streamConfig.MaxPlayersPerStream,
	}, sol.channel, &sol.wg)

	// RTSP 서버 생성 시 Sol 서버의 채널과 WaitGroup 전달
	sol.rtsp = rtsp.NewServer(rtsp.RTSPConfig{
		Port:    rtspPort,
		Timeout: rtspTimeout,
	}, sol.channel, &sol.wg)
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
	s.cancel()
	slog.Info("Sol Server stop signal sent")
}

func (s *Server) eventLoop() {
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
		slog.Warn("Unknown event type", "eventType", fmt.Sprintf("%T", v))
	}
}

// shutdown performs the actual shutdown sequence
func (s *Server) shutdown() {
	slog.Info("Sol event loop stopping...")

	s.rtmp.Stop()
	slog.Info("RTMP Server stopped")

	s.rtsp.Stop()
	slog.Info("RTSP Server stopped")

	// 모든 송신자(eventLoop)가 완료될 때까지 대기
	s.wg.Wait()
	slog.Info("All senders finished")

	slog.Info("Sol Server stopped successfully")
}
