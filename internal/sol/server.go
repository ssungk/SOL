package sol

import (
	"log/slog"
	"sol/pkg/rtmp"
	"time"
)

type Server struct {
	ticker  *time.Ticker
	rtmp    *rtmp.Server
	channel chan interface{}
	done    chan struct{} // 종료 신호 채널
}

func NewServer() *Server {
	InitLogger()

	sol := &Server{
		channel: make(chan interface{}, 10),
		rtmp:    rtmp.NewServer(),
		ticker:  time.NewTicker(1000 * time.Second),
		done:    make(chan struct{}), // 종료 신호 채널 초기화
	}
	return sol
}

func (s *Server) Start() error {
	slog.Info("Start Server")
	err := s.rtmp.Start()
	if err != nil {
		return err
	}

	// 이벤트 루프를 고루틴으로 시작
	go s.eventLoop()
	return nil
}

func (s *Server) Stop() {
	slog.Info("Stopping Sol Server...")
	
	// 1. RTMP 서버 종료
	s.rtmp.Stop()
	
	// 2. 티커 종료
	if s.ticker != nil {
		s.ticker.Stop()
		slog.Info("Ticker stopped")
	}
	
	// 3. 이벤트 루프 종료
	close(s.done)
	
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
		case <-s.done:
			slog.Info("Sol event loop stopping...")
			return
		}
	}
}

func (s *Server) channelHandler(data interface{}) {

}
