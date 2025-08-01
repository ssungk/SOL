package rtsp

import (
	"log/slog"
	"sync"
)

// Stream represents an RTSP stream
type Stream struct {
	name      string
	sessions  map[*Session]struct{} // connected sessions
	publisher *Session              // publishing session (for RECORD)
	players   map[*Session]struct{} // playing sessions
	sdp       string                // Session Description Protocol
	isActive  bool
	mutex     sync.RWMutex
}

// StreamManager manages RTSP streams
type StreamManager struct {
	streams map[string]*Stream
	mutex   sync.RWMutex
}

// NewStreamManager creates a new stream manager
func NewStreamManager() *StreamManager {
	return &StreamManager{
		streams: make(map[string]*Stream),
	}
}

// NewStream creates a new RTSP stream
func NewStream(name string) *Stream {
	return &Stream{
		name:     name,
		sessions: make(map[*Session]struct{}),
		players:  make(map[*Session]struct{}),
		isActive: false,
	}
}

// GetOrCreateStream gets or creates a stream
func (sm *StreamManager) GetOrCreateStream(streamPath string) *Stream {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	stream, exists := sm.streams[streamPath]
	if !exists {
		stream = NewStream(streamPath)
		sm.streams[streamPath] = stream
		slog.Info("RTSP stream created", "streamPath", streamPath)
	}

	return stream
}

// GetStream gets a stream by path
func (sm *StreamManager) GetStream(streamPath string) *Stream {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.streams[streamPath]
}

// RemoveStream removes a stream
func (sm *StreamManager) RemoveStream(streamPath string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	delete(sm.streams, streamPath)
	slog.Info("RTSP stream removed", "streamPath", streamPath)
}

// GetAllStreams returns all streams
func (sm *StreamManager) GetAllStreams() map[string]*Stream {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	result := make(map[string]*Stream)
	for path, stream := range sm.streams {
		result[path] = stream
	}

	return result
}

// AddSession adds a session to the stream
func (s *Stream) AddSession(session *Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.sessions[session] = struct{}{}
	slog.Info("Session added to RTSP stream", "streamPath", s.name, "sessionId", session.sessionId, "sessionCount", len(s.sessions))
}

// RemoveSession removes a session from the stream
func (s *Stream) RemoveSession(session *Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.sessions, session)
	delete(s.players, session)

	// Clear publisher if it's the same session
	if s.publisher == session {
		s.publisher = nil
		s.isActive = false
		slog.Info("Publisher removed from RTSP stream", "streamPath", s.name)
	}

	slog.Info("Session removed from RTSP stream", "streamPath", s.name, "sessionId", session.sessionId, "sessionCount", len(s.sessions))
}

// SetPublisher sets the publishing session
func (s *Stream) SetPublisher(session *Session, sdp string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.publisher = session
	s.sdp = sdp
	s.isActive = true

	slog.Info("Publisher set for RTSP stream", "streamPath", s.name, "sessionId", session.sessionId)
}

// AddPlayer adds a playing session
func (s *Stream) AddPlayer(session *Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.players[session] = struct{}{}
	slog.Info("Player added to RTSP stream", "streamPath", s.name, "sessionId", session.sessionId, "playerCount", len(s.players))
}

// RemovePlayer removes a playing session
func (s *Stream) RemovePlayer(session *Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.players, session)
	slog.Info("Player removed from RTSP stream", "streamPath", s.name, "sessionId", session.sessionId, "playerCount", len(s.players))
}

// GetSDP returns the SDP for the stream
func (s *Stream) GetSDP() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.sdp
}

// IsActive returns whether the stream is active
func (s *Stream) IsActive() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.isActive
}

// GetPlayerCount returns the number of players
func (s *Stream) GetPlayerCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return len(s.players)
}

// GetSessionCount returns the total number of sessions
func (s *Stream) GetSessionCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return len(s.sessions)
}

// BroadcastRTPPacket broadcasts RTP packet to all players
func (s *Stream) BroadcastRTPPacket(data []byte) {
	s.mutex.RLock()
	players := make([]*Session, 0, len(s.players))
	for player := range s.players {
		players = append(players, player)
	}
	s.mutex.RUnlock()

	// Send RTP packet to all players
	for _, player := range players {
		if player.IsInterleavedMode() {
			// TCP interleaved mode
			err := player.SendInterleavedRTPPacket(data)
			if err != nil {
				slog.Error("Failed to send interleaved RTP packet to player",
					"streamPath", s.name, "sessionId", player.sessionId, "err", err)
			} else {
				slog.Debug("Interleaved RTP packet sent to player",
					"streamPath", s.name, "sessionId", player.sessionId, "dataSize", len(data))
			}
		} else if player.IsUDPMode() && player.rtpSession != nil && player.rtpTransport != nil {
			// UDP mode
			err := player.rtpTransport.SendRTPPacket(player.rtpSession.GetSSRC(), data, 0, false)
			if err != nil {
				slog.Error("Failed to send UDP RTP packet to player",
					"streamPath", s.name, "sessionId", player.sessionId, "err", err)
			} else {
				slog.Debug("UDP RTP packet sent to player",
					"streamPath", s.name, "sessionId", player.sessionId, "dataSize", len(data))
			}
		} else {
			slog.Debug("Player has no valid transport setup", "streamPath", s.name, "sessionId", player.sessionId)
		}
	}
}

// CleanupInactiveSessions removes inactive sessions
func (s *Stream) CleanupInactiveSessions() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// This would typically check for inactive sessions and remove them
	// For now, it's a placeholder
}
