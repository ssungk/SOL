package sol

import (
	"log/slog"
	"time"
)

type Server struct {
	ticker  *time.Ticker
	channel chan interface{}
}

func NewServer() *Server {
	InitLogger()

	rtmp := &Server{
		channel: make(chan interface{}, 10),
		ticker:  time.NewTicker(10 * time.Second),
	}
	return rtmp
}

func (s *Server) Start() {
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
