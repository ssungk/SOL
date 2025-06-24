package rtmp

import (
	"io"
	"log/slog"
	"net"
)

type Server struct {
	sessions map[*session]struct{}
	port     int
	channel  chan interface{}
}

func NewServer() *Server {
	rtmp := &Server{
		sessions: make(map[*session]struct{}),
		port:     1935,
	}
	return rtmp
}

func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}

	go s.acceptConnections(ln)

	return nil
}

func (s *Server) Stop() {

}

func (s *Server) eventLoop() {
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		}
	}
}

func (s *Server) channelHandler(data interface{}) {
	switch v := data.(type) {
	case Terminated:
		s.TerminatedEventHandler(v.Id)
	default: //logError("Received unknown data type")
	}
}

func (s *Server) TerminatedEventHandler(id string) {

}

func (s *Server) createListener() (net.Listener, error) {
	ln, err := net.Listen("tcp", ":1935")
	if err != nil {
		slog.Info("Error starting RTMP server", "err", err)
		return nil, err
	}

	return ln, nil
}

func (s *Server) acceptConnections(ln net.Listener) {
	defer closeWithLog(ln)
	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Error("Accept failed", "err", err)
			// TODO: 종료 로직 필요
			return
		}
		session := newSession(conn)
		s.sessions[session] = struct{}{}
	}
}

func closeWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "err", err)
	}
}
