package sol

import (
	"log/slog"
	"os"
	"os/signal"
	"sol/internal/api"
	"sol/internal/media"
	"strconv"
	"syscall"
)

// SOL represents the main application
type SOL struct {
	config      *Config
	mediaServer *media.MediaServer
	apiServer   *api.Server
}

// NewApp creates a new application instance
func NewSol() *SOL {
	// 설정 로드
	config, err := LoadConfig()
	if err != nil {
		slog.Error("Failed to load config", "err", err)
		os.Exit(1)
	}

	// 설정을 기반으로 로거 초기화
	InitLogger(config)

	// media 서버 생성 (RTMP, RTSP, SRT)
	mediaServer := media.NewMediaServer(config.RTMP.Port, config.RTSP.Port, config.RTSP.Timeout, config.SRT.Port, config.SRT.Timeout)

	// API 서버 생성 (media 서버를 DI)
	apiServer := api.NewServer(strconv.Itoa(config.API.Port), mediaServer)

	return &SOL{
		config:      config,
		mediaServer: mediaServer,
		apiServer:   apiServer,
	}
}

// Start starts the application
func (app *SOL) Start() {
	slog.Info("Application starting...")

	// Media 서버 시작 (RTMP, RTSP, SRT)
	if err := app.mediaServer.Start(); err != nil {
		slog.Error("Failed to start media server", "err", err)
		os.Exit(1)
	}

	// API 서버 시작
	if err := app.apiServer.Start(); err != nil {
		slog.Error("Failed to start API server", "err", err)
		os.Exit(1)
	}

	slog.Info("API Server started", "port", app.config.API.Port)

	// 시그널 처리
	app.waitForShutdown()
}

// waitForShutdown waits for shutdown signals and performs graceful shutdown
func (app *SOL) waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	slog.Info("Received signal, shutting down application", "signal", sig)

	app.shutdown()
}

// shutdown performs graceful shutdown
func (app *SOL) shutdown() {
	slog.Info("Stopping application...")

	// Media 서버 종료 (graceful shutdown 내장)
	app.mediaServer.Stop()

	// API 서버는 graceful shutdown 구현 시 여기서 처리
	// 현재는 Media 서버 종료 시 프로세스가 종료됨
	slog.Info("Application stopped successfully")
}
