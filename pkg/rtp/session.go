package rtp

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
)

// RTPSession represents a simple RTP session for UDP transmission
type RTPSession struct {
	SSRC           uint32
	sequenceNumber uint32
	payloadType    uint8
	clientRTPAddr  *net.UDPAddr
	active         bool
	mu             sync.RWMutex
}

// RTPTransport handles RTP transport over UDP (simplified)
type RTPTransport struct {
	rtpListener net.PacketConn
	sessions    map[uint32]*RTPSession // SSRC -> Session
	mu          sync.RWMutex
}

// NewRTPSession creates a new RTP session
func NewRTPSession(ssrc uint32, payloadType uint8) *RTPSession {
	return &RTPSession{
		SSRC:        ssrc,
		payloadType: payloadType,
		active:      true,
	}
}

// NewRTPTransport creates a new RTP transport
func NewRTPTransport() *RTPTransport {
	return &RTPTransport{
		sessions: make(map[uint32]*RTPSession),
	}
}

// StartUDP starts UDP listener for RTP
func (t *RTPTransport) StartUDP(rtpPort int) error {
	// Start RTP listener
	rtpAddr := fmt.Sprintf(":%d", rtpPort)
	rtpListener, err := net.ListenPacket("udp", rtpAddr)
	if err != nil {
		return fmt.Errorf("failed to start RTP listener on %s: %v", rtpAddr, err)
	}
	t.rtpListener = rtpListener
	
	slog.Info("RTP transport started", "rtpPort", rtpPort)
	return nil
}

// Stop stops the RTP transport
func (t *RTPTransport) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if t.rtpListener != nil {
		t.rtpListener.Close()
	}
	
	// Close all sessions
	for _, session := range t.sessions {
		session.Close()
	}
	
	t.sessions = make(map[uint32]*RTPSession)
	slog.Info("RTP transport stopped")
}

// CreateSession creates a new RTP session
func (t *RTPTransport) CreateSession(ssrc uint32, payloadType uint8, clientRTPPort int, clientIP string) (*RTPSession, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	session := NewRTPSession(ssrc, payloadType)
	
	// Parse client address
	clientRTPAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", clientIP, clientRTPPort))
	if err != nil {
		return nil, fmt.Errorf("invalid client RTP address: %v", err)
	}
	
	session.clientRTPAddr = clientRTPAddr
	
	// Store session
	t.sessions[ssrc] = session
	
	slog.Info("RTP session created", "ssrc", ssrc, "payloadType", payloadType, "clientRTP", clientRTPAddr)
	
	return session, nil
}

// GetSession returns an RTP session by SSRC
func (t *RTPTransport) GetSession(ssrc uint32) *RTPSession {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessions[ssrc]
}

// RemoveSession removes an RTP session
func (t *RTPTransport) RemoveSession(ssrc uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if session, exists := t.sessions[ssrc]; exists {
		session.Close()
		delete(t.sessions, ssrc)
		slog.Info("RTP session removed", "ssrc", ssrc)
	}
}

// SendRTPPacket sends an RTP packet to the client
func (t *RTPTransport) SendRTPPacket(ssrc uint32, payload []byte, timestamp uint32, marker bool) error {
	session := t.GetSession(ssrc)
	if session == nil {
		return fmt.Errorf("RTP session not found: %d", ssrc)
	}
	
	return session.SendRTPPacket(payload, timestamp, marker, t.rtpListener)
}

// SendRTPPacket sends an RTP packet
func (s *RTPSession) SendRTPPacket(payload []byte, timestamp uint32, marker bool, listener net.PacketConn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if !s.active {
		return fmt.Errorf("RTP session is not active")
	}
	
	// Create RTP packet
	seqNum := uint16(atomic.AddUint32(&s.sequenceNumber, 1))
	packet := NewRTPPacket(s.payloadType, seqNum, timestamp, s.SSRC, payload)
	packet.SetMarker(marker)
	
	// Marshal packet
	data, err := packet.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal RTP packet: %v", err)
	}
	
	// Send to client
	_, err = listener.WriteTo(data, s.clientRTPAddr)
	if err != nil {
		return fmt.Errorf("failed to send RTP packet: %v", err)
	}
	
	slog.Debug("RTP packet sent", "ssrc", s.SSRC, "seq", seqNum, "ts", timestamp, "size", len(data))
	return nil
}

// Close closes the RTP session
func (s *RTPSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.active = false
	slog.Info("RTP session closed", "ssrc", s.SSRC)
}

// GetSSRC returns the SSRC
func (s *RTPSession) GetSSRC() uint32 {
	return s.SSRC
}

// GetPayloadType returns the payload type
func (s *RTPSession) GetPayloadType() uint8 {
	return s.payloadType
}
