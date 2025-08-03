package media

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

type MediaServer struct {
	rtmp    *rtmp.Server
	rtsp    *rtsp.Server
	channel chan interface{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup // 송신자들을 추적하기 위한 WaitGroup
}

func NewMediaServer(rtmpPort, rtspPort, rtspTimeout int, streamConfig StreamConfig) *MediaServer {
	// 자체적으로 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	mediaServer := &MediaServer{
		channel: make(chan interface{}, 10),
		ctx:     ctx,
		cancel:  cancel,
	}

	// RTMP 서버 생성 시 MediaServer의 채널과 WaitGroup 전달
	mediaServer.rtmp = rtmp.NewServer(rtmpPort, rtmp.StreamConfig{
		GopCacheSize:        streamConfig.GopCacheSize,
		MaxPlayersPerStream: streamConfig.MaxPlayersPerStream,
	}, mediaServer.channel, &mediaServer.wg)

	// RTSP 서버 생성 시 MediaServer의 채널과 WaitGroup 전달
	mediaServer.rtsp = rtsp.NewServer(rtsp.RTSPConfig{
		Port:    rtspPort,
		Timeout: rtspTimeout,
	}, mediaServer.channel, &mediaServer.wg)
	return mediaServer
}

func (s *MediaServer) Start() error {
	slog.Info("Media servers starting...")

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
func (s *MediaServer) Stop() {
	slog.Info("Stopping Media Server...")
	s.cancel()
	slog.Info("Media Server stop signal sent")
}

func (s *MediaServer) eventLoop() {
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

func (s *MediaServer) channelHandler(data interface{}) {
	switch v := data.(type) {
	default:
		slog.Warn("Unknown event type", "eventType", fmt.Sprintf("%T", v))
	}
}

// shutdown performs the actual shutdown sequence
func (s *MediaServer) shutdown() {
	slog.Info("Media event loop stopping...")

	s.rtmp.Stop()
	slog.Info("RTMP Server stopped")

	s.rtsp.Stop()
	slog.Info("RTSP Server stopped")

	// 모든 송신자(eventLoop)가 완료될 때까지 대기
	s.wg.Wait()
	slog.Info("All senders finished")

	slog.Info("Media Server stopped successfully")
}
