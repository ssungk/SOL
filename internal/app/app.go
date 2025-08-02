package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sol/internal/api"
	"sol/internal/sol"
	"strconv"
	"syscall"
)

// App represents the main application
type App struct {
	config    *Config
	solServer *sol.Server
	apiServer *api.Server
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewApp creates a new application instance
func NewApp() *App {
	// 설정 로드
	config, err := LoadConfig()
	if err != nil {
		slog.Error("Failed to load config", "err", err)
		os.Exit(1)
	}

	// 설정을 기반으로 로거 초기화
	InitLogger(config)

	// 취소 가능한 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())

	// sol 서버 생성 (RTMP, RTSP)
	solServer := sol.NewServer(config.RTMP.Port, config.RTSP.Port, config.RTSP.Timeout, sol.StreamConfig{
		GopCacheSize:        config.Stream.GopCacheSize,
		MaxPlayersPerStream: config.Stream.MaxPlayersPerStream,
	}, ctx)

	// API 서버 생성 (sol 서버를 DI)
	apiServer := api.NewServerWithDI(strconv.Itoa(config.API.Port), solServer)

	return &App{
		config:    config,
		solServer: solServer,
		apiServer: apiServer,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start starts the application
func (app *App) Start() {
	slog.Info("Application starting...")

	// Sol 서버 시작 (RTMP, RTSP)
	if err := app.solServer.Start(); err != nil {
		slog.Error("Failed to start sol server", "err", err)
		os.Exit(1)
	}

	// API 서버 시작
	go func() {
		if err := app.apiServer.Start(); err != nil {
			slog.Error("Failed to start API server", "err", err)
			os.Exit(1)
		}
	}()

	slog.Info("API Server started", "port", app.config.API.Port)

	// 시그널 처리
	app.waitForShutdown()
}

// waitForShutdown waits for shutdown signals and performs graceful shutdown
func (app *App) waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		slog.Info("Received signal, shutting down application", "signal", sig)
	case <-app.ctx.Done():
		slog.Info("Context cancelled, shutting down application")
	}

	app.shutdown()
}

// shutdown performs graceful shutdown
func (app *App) shutdown() {
	slog.Info("Stopping application...")

	// 컨텍스트 취소
	app.cancel()

	// Sol 서버 종료
	app.solServer.Stop()

	// API 서버는 graceful shutdown 구현 시 여기서 처리
	// 현재는 Sol 서버 종료 시 프로세스가 종료됨

	slog.Info("Application stopped successfully")
}
