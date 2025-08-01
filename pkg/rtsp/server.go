package rtsp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
)

// RTSPConfig represents RTSP server configuration
type RTSPConfig struct {
	Port    int
	Timeout int // seconds
}

// Server represents an RTSP server
type Server struct {
	port          int
	timeout       int
	sessions      map[string]*Session // sessionId -> session
	streamManager *StreamManager
	channel       chan interface{}
	listener      net.Listener
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewServer creates a new RTSP server
func NewServer(config RTSPConfig) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Server{
		port:          config.Port,
		timeout:       config.Timeout,
		sessions:      make(map[string]*Session),
		streamManager: NewStreamManager(),
		channel:       make(chan interface{}, 100),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start starts the RTSP server
func (s *Server) Start() error {
	ln, err := s.createListener()
	if err != nil {
		return err
	}
	s.listener = ln
	
	// Start event loop
	go s.eventLoop()
	
	// Start accepting connections
	go s.acceptConnections(ln)
	
	return nil
}

// Stop stops the RTSP server
func (s *Server) Stop() {
	slog.Info("RTSP Server stopping...")
	
	// Cancel context
	s.cancel()
	
	// Close listener
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Error("Error closing RTSP listener", "err", err)
		} else {
			slog.Info("RTSP Listener closed")
		}
	}
	
	// Close all sessions
	slog.Info("Closing all RTSP sessions", "sessionCount", len(s.sessions))
	for sessionId, session := range s.sessions {
		session.Stop()
		slog.Debug("RTSP session stopped", "sessionId", sessionId)
	}
	
	// Clear data structures
	s.sessions = make(map[string]*Session)
	
	// Clean up channel
	for {
		select {
		case <-s.channel:
			// Drain remaining events
		default:
			goto cleanup_done
		}
	}
	
cleanup_done:
	close(s.channel)
	slog.Info("RTSP Server stopped successfully")
}

// eventLoop processes events
func (s *Server) eventLoop() {
	for {
		select {
		case event := <-s.channel:
			s.handleEvent(event)
		case <-s.ctx.Done():
			slog.Info("RTSP Event loop stopping...")
			return
		}
	}
}

// handleEvent handles different types of events
func (s *Server) handleEvent(event interface{}) {
	switch e := event.(type) {
	case SessionTerminated:
		s.handleSessionTerminated(e)
	case DescribeRequested:
		s.handleDescribeRequested(e)
	case PlayStarted:
		s.handlePlayStarted(e)
	case PlayStopped:
		s.handlePlayStopped(e)
	case RecordStarted:
		s.handleRecordStarted(e)
	case RecordStopped:
		s.handleRecordStopped(e)
	case AnnounceReceived:
		s.handleAnnounceReceived(e)
	case RTPPacketReceived:
		s.handleRTPPacketReceived(e)
	default:
		slog.Warn("Unknown RTSP event type", "eventType", fmt.Sprintf("%T", e))
	}
}

// handleSessionTerminated handles session termination
func (s *Server) handleSessionTerminated(event SessionTerminated) {
	session := s.sessions[event.SessionId]
	if session == nil {
		slog.Warn("Session not found for termination", "sessionId", event.SessionId)
		return
	}

	// Remove session from all streams
	for _, stream := range s.streamManager.GetAllStreams() {
		stream.RemoveSession(session)
		
		// Remove inactive streams
		if stream.GetSessionCount() == 0 {
			s.streamManager.RemoveStream(stream.name)
		}
	}

	// Remove session from server
	delete(s.sessions, event.SessionId)
	slog.Info("RTSP session terminated", "sessionId", event.SessionId)
}

// handleDescribeRequested handles DESCRIBE requests
func (s *Server) handleDescribeRequested(event DescribeRequested) {
	slog.Info("DESCRIBE requested", "sessionId", event.SessionId, "streamPath", event.StreamPath)
	
	// Get or create stream
	stream := s.streamManager.GetOrCreateStream(event.StreamPath)
	
	// Add session to stream
	if session := s.sessions[event.SessionId]; session != nil {
		stream.AddSession(session)
	}
}

// handlePlayStarted handles PLAY requests
func (s *Server) handlePlayStarted(event PlayStarted) {
	slog.Info("PLAY started", "sessionId", event.SessionId, "streamPath", event.StreamPath)
	
	// Get stream
	stream := s.streamManager.GetStream(event.StreamPath)
	if stream == nil {
		slog.Warn("Stream not found for PLAY", "streamPath", event.StreamPath)
		return
	}
	
	// Add session as player
	if session := s.sessions[event.SessionId]; session != nil {
		stream.AddPlayer(session)
	}
}

// handlePlayStopped handles PLAY stop/PAUSE/TEARDOWN
func (s *Server) handlePlayStopped(event PlayStopped) {
	slog.Info("PLAY stopped", "sessionId", event.SessionId, "streamPath", event.StreamPath)
	
	// Get stream
	stream := s.streamManager.GetStream(event.StreamPath)
	if stream == nil {
		return
	}
	
	// Remove session as player
	if session := s.sessions[event.SessionId]; session != nil {
		stream.RemovePlayer(session)
	}
}

// handleRecordStarted handles RECORD requests
func (s *Server) handleRecordStarted(event RecordStarted) {
	slog.Info("RECORD started", "sessionId", event.SessionId, "streamPath", event.StreamPath)
	
	// Get or create stream
	stream := s.streamManager.GetOrCreateStream(event.StreamPath)
	
	// Set session as publisher
	if session := s.sessions[event.SessionId]; session != nil {
		stream.SetPublisher(session, "")
	}
}

// handleRecordStopped handles RECORD stop
func (s *Server) handleRecordStopped(event RecordStopped) {
	slog.Info("RECORD stopped", "sessionId", event.SessionId, "streamPath", event.StreamPath)
	
	// Implementation would handle stopping recording
}

// handleAnnounceReceived handles ANNOUNCE with SDP
func (s *Server) handleAnnounceReceived(event AnnounceReceived) {
	slog.Info("ANNOUNCE received", "sessionId", event.SessionId, "streamPath", event.StreamPath)
	
	// Get or create stream
	stream := s.streamManager.GetOrCreateStream(event.StreamPath)
	
	// Set session as publisher with SDP
	if session := s.sessions[event.SessionId]; session != nil {
		stream.SetPublisher(session, event.SDP)
		stream.AddSession(session)
	}
}

// handleRTPPacketReceived handles RTP packets
func (s *Server) handleRTPPacketReceived(event RTPPacketReceived) {
	slog.Debug("RTP packet received", "sessionId", event.SessionId, "streamPath", event.StreamPath, "dataSize", len(event.Data))
	
	// Get stream
	stream := s.streamManager.GetStream(event.StreamPath)
	if stream == nil {
		return
	}
	
	// Broadcast to all players
	stream.BroadcastRTPPacket(event.Data)
}

// createListener creates a TCP listener
func (s *Server) createListener() (net.Listener, error) {
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("Error starting RTSP server", "err", err)
		return nil, err
	}
	
	return ln, nil
}

// acceptConnections accepts incoming connections
func (s *Server) acceptConnections(ln net.Listener) {
	defer closeWithLog(ln)
	
	for {
		// Check for context cancellation
		select {
		case <-s.ctx.Done():
			slog.Info("RTSP accept loop stopping...")
			return
		default:
		}
		
		conn, err := ln.Accept()
		if err != nil {
			// Check if listener was closed
			select {
			case <-s.ctx.Done():
				slog.Info("RTSP accept loop stopped (listener closed)")
				return
			default:
				slog.Error("RTSP accept failed", "err", err)
				return
			}
		}
		
		// Create new session
		session := NewSession(conn, s.channel)
		s.sessions[session.sessionId] = session
		
		// Start session handling
		session.Start()
		
		slog.Info("New RTSP session created", "sessionId", session.sessionId, "remoteAddr", conn.RemoteAddr())
	}
}

// closeWithLog closes a resource with logging
func closeWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "err", err)
	}
}
