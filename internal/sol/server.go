package sol

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sol/pkg/rtmp"
	"syscall"
	"time"
)

type Server struct {
	ticker  *time.Ticker
	rtmp    *rtmp.Server
	channel chan interface{}
	ctx     context.Context    // 루트 컨텍스트
	cancel  context.CancelFunc // 컨텍스트 취소 함수
	config  *Config            // 설정
}

func NewServer() *Server {
	// 설정 로드 (로거 초기화 전에 먼저)
	config, err := LoadConfig()
	if err != nil {
		// 설정 로드 실패 시 기본 로거로 에러 출력
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 설정을 기반으로 로거 초기화
	InitLogger(config)

	// 취소 가능한 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	sol := &Server{
		channel: make(chan interface{}, 10),
		rtmp:    rtmp.NewServer(config.RTMP.Port, rtmp.StreamConfig{
			GopCacheSize:        config.Stream.GopCacheSize,
			MaxPlayersPerStream: config.Stream.MaxPlayersPerStream,
		}),
		ticker:  time.NewTicker(1000 * time.Second),
		ctx:     ctx,
		cancel:  cancel,
		config:  config,
	}
	return sol
}

func (s *Server) Start() {
	slog.Info("RTMP Server starting...")
	
	// RTMP 서버 시작
	if err := s.rtmp.Start(); err != nil {
		slog.Error("Failed to start RTMP server", "err", err)
		os.Exit(1)
	}
	
	slog.Info("RTMP Server started", "port", s.config.RTMP.Port)
	
	// 이벤트 루프 시작
	go s.eventLoop()
	
	// 시그널 처리 시작
	s.waitForShutdown()
}

// waitForShutdown은 시그널을 대기하고 우아한 종료를 수행합니다
func (s *Server) waitForShutdown() {
	// 시그널 채널 생성
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// 시그널 대기
	select {
	case sig := <-sigChan:
		slog.Info("Received signal, shutting down server", "signal", sig)
	case <-s.ctx.Done():
		slog.Info("Context cancelled, shutting down server")
	}
	
	// 우아한 종료 수행
	s.shutdown()
}

// shutdown은 실제 종료 로직을 수행합니다
func (s *Server) shutdown() {
	slog.Info("Stopping Sol Server...")
	
	// 1. 컨텍스트 취소 (모든 고루틴에 종료 신호)
	s.cancel()
	
	// 2. RTMP 서버 종료
	s.rtmp.Stop()
	
	// 3. 티커 종료
	if s.ticker != nil {
		s.ticker.Stop()
		slog.Info("Ticker stopped")
	}
	
	// 4. 채널 청소
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
