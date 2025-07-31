package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sol/internal/sol"
	"syscall"
)

func main() {
	server := sol.NewServer()
	
	// 서버 시작
	if err := server.Start(); err != nil {
		slog.Error("Failed to start server", "err", err)
		os.Exit(1)
	}
	
	slog.Info("RTMP Server started on port 1935")
	
	// 시그널 수신을 위한 채널 생성
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// 시그널 대기
	sig := <-sigChan
	slog.Info("Received signal, shutting down server", "signal", sig)
	
	// 서버 정지
	server.Stop()
	slog.Info("Server shutdown complete")
}
