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
}

func NewServer() *Server {
	InitLogger()

	sol := &Server{
		channel: make(chan interface{}, 10),
		rtmp:    rtmp.NewServer(),
		ticker:  time.NewTicker(1000 * time.Second),
	}
	return sol
}

func (s *Server) Start() {
	slog.Info("Start Server")
	err := s.rtmp.Start()
	if err != nil {
		return
	}

	s.eventLoop()
}

func (s *Server) Stop() {

}

func (s *Server) eventLoop() {
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		case <-s.ticker.C:
			slog.Info("test")
		}
	}
}

func (s *Server) channelHandler(data interface{}) {

}
