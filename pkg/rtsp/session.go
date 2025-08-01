package rtsp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// Session represents an RTSP client session
type Session struct {
	sessionId       string
	conn            net.Conn
	reader          *MessageReader
	writer          *MessageWriter
	cseq            int
	state           SessionState
	streamPath      string
	clientPorts     []int          // RTP and RTCP ports
	serverPorts     []int          // RTP and RTCP ports
	transport       string
	timeout         time.Duration
	lastActivity    time.Time
	externalChannel chan interface{}
	ctx             context.Context
	cancel          context.CancelFunc
}

// SessionState represents the current state of an RTSP session
type SessionState int

const (
	StateInit SessionState = iota
	StateReady
	StatePlaying
	StateRecording
)

// String returns the string representation of the session state
func (s SessionState) String() string {
	switch s {
	case StateInit:
		return "Init"
	case StateReady:
		return "Ready"
	case StatePlaying:
		return "Playing"
	case StateRecording:
		return "Recording"
	default:
		return "Unknown"
	}
}

// NewSession creates a new RTSP session
func NewSession(conn net.Conn, externalChannel chan interface{}) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	
	session := &Session{
		conn:            conn,
		reader:          NewMessageReader(conn),
		writer:          NewMessageWriter(conn),
		cseq:            0,
		state:           StateInit,
		timeout:         DefaultTimeout * time.Second,
		lastActivity:    time.Now(),
		externalChannel: externalChannel,
		ctx:             ctx,
		cancel:          cancel,
	}
	
	// 포인터 주소값을 sessionId로 사용
	session.sessionId = fmt.Sprintf("%p", session)
	
	return session
}

// Start starts the session handling
func (s *Session) Start() {
	slog.Info("RTSP session started", "sessionId", s.sessionId, "remoteAddr", s.conn.RemoteAddr())
	
	go s.handleRequests()
	go s.handleTimeout()
}

// Stop stops the session
func (s *Session) Stop() {
	slog.Info("RTSP session stopping", "sessionId", s.sessionId)
	
	// Cancel context
	s.cancel()
	
	// Close connection
	if s.conn != nil {
		s.conn.Close()
	}
	
	// Send termination event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- SessionTerminated{SessionId: s.sessionId}:
		default:
		}
	}
}

// handleRequests handles incoming RTSP requests
func (s *Session) handleRequests() {
	defer s.Stop()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		
		// Set read timeout
		s.conn.SetReadDeadline(time.Now().Add(s.timeout))
		
		request, err := s.reader.ReadRequest()
		if err != nil {
			slog.Error("Failed to read RTSP request", "sessionId", s.sessionId, "err", err)
			return
		}
		
		s.lastActivity = time.Now()
		slog.Debug("RTSP request received", "sessionId", s.sessionId, "method", request.Method, "uri", request.URI, "cseq", request.CSeq)
		
		if err := s.handleRequest(request); err != nil {
			slog.Error("Failed to handle RTSP request", "sessionId", s.sessionId, "method", request.Method, "err", err)
			s.sendErrorResponse(request.CSeq, StatusInternalServerError)
		}
	}
}

// handleTimeout handles session timeout
func (s *Session) handleTimeout() {
	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer ticker.Stop()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if time.Since(s.lastActivity) > s.timeout {
				slog.Info("RTSP session timed out", "sessionId", s.sessionId)
				s.Stop()
				return
			}
		}
	}
}

// handleRequest handles a specific RTSP request
func (s *Session) handleRequest(req *Request) error {
	switch req.Method {
	case MethodOptions:
		return s.handleOptions(req)
	case MethodDescribe:
		return s.handleDescribe(req)
	case MethodSetup:
		return s.handleSetup(req)
	case MethodPlay:
		return s.handlePlay(req)
	case MethodTeardown:
		return s.handleTeardown(req)
	case MethodPause:
		return s.handlePause(req)
	case MethodRecord:
		return s.handleRecord(req)
	case MethodAnnounce:
		return s.handleAnnounce(req)
	default:
		return s.sendErrorResponse(req.CSeq, StatusMethodNotAllowed)
	}
}

// handleOptions handles OPTIONS request
func (s *Session) handleOptions(req *Request) error {
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderPublic, "OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY, PAUSE, ANNOUNCE, RECORD")
	response.SetHeader(HeaderServer, "Sol RTSP Server")
	
	return s.writer.WriteResponse(response)
}

// handleDescribe handles DESCRIBE request
func (s *Session) handleDescribe(req *Request) error {
	s.streamPath = req.URI
	
	// Send DESCRIBE event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- DescribeRequested{
			SessionId:  s.sessionId,
			StreamPath: s.streamPath,
		}:
		default:
		}
	}
	
	// For now, return a simple SDP (Session Description Protocol)
	sdp := s.generateSDP()
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderContentType, "application/sdp")
	response.SetHeader(HeaderContentLength, strconv.Itoa(len(sdp)))
	response.Body = []byte(sdp)
	
	return s.writer.WriteResponse(response)
}

// handleSetup handles SETUP request
func (s *Session) handleSetup(req *Request) error {
	// Parse transport header
	transportHeader := req.GetHeader(HeaderTransport)
	if transportHeader == "" {
		return s.sendErrorResponse(req.CSeq, StatusBadRequest)
	}
	
	s.transport = transportHeader
	s.parseTransport(transportHeader)
	
	// Generate server ports (for now, use dummy ports)
	s.serverPorts = []int{8000, 8001} // RTP, RTCP
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderTransport, s.buildTransportResponse())
	response.SetHeader(HeaderSession, fmt.Sprintf("%s;timeout=%d", s.sessionId, int(s.timeout.Seconds())))
	
	s.state = StateReady
	
	return s.writer.WriteResponse(response)
}

// handlePlay handles PLAY request
func (s *Session) handlePlay(req *Request) error {
	if s.state != StateReady {
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}
	
	// Send PLAY event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- PlayStarted{
			SessionId:  s.sessionId,
			StreamPath: s.streamPath,
		}:
		default:
		}
	}
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, s.sessionId)
	response.SetHeader(HeaderRTPInfo, fmt.Sprintf("url=%s;seq=0;rtptime=0", req.URI))
	
	s.state = StatePlaying
	
	return s.writer.WriteResponse(response)
}

// handlePause handles PAUSE request
func (s *Session) handlePause(req *Request) error {
	if s.state != StatePlaying {
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}
	
	// Send PAUSE event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- PlayStopped{
			SessionId:  s.sessionId,
			StreamPath: s.streamPath,
		}:
		default:
		}
	}
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, s.sessionId)
	
	s.state = StateReady
	
	return s.writer.WriteResponse(response)
}

// handleTeardown handles TEARDOWN request
func (s *Session) handleTeardown(req *Request) error {
	// Send TEARDOWN event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- PlayStopped{
			SessionId:  s.sessionId,
			StreamPath: s.streamPath,
		}:
		default:
		}
	}
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, s.sessionId)
	
	s.state = StateInit
	
	// Schedule session termination after response
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Stop()
	}()
	
	return s.writer.WriteResponse(response)
}

// handleRecord handles RECORD request
func (s *Session) handleRecord(req *Request) error {
	if s.state != StateReady {
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}
	
	// Send RECORD event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- RecordStarted{
			SessionId:  s.sessionId,
			StreamPath: s.streamPath,
		}:
		default:
		}
	}
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, s.sessionId)
	
	s.state = StateRecording
	
	return s.writer.WriteResponse(response)
}

// handleAnnounce handles ANNOUNCE request
func (s *Session) handleAnnounce(req *Request) error {
	s.streamPath = req.URI
	
	// Send ANNOUNCE event
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- AnnounceReceived{
			SessionId:  s.sessionId,
			StreamPath: s.streamPath,
			SDP:        string(req.Body),
		}:
		default:
		}
	}
	
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	
	return s.writer.WriteResponse(response)
}

// sendErrorResponse sends an error response
func (s *Session) sendErrorResponse(cseq int, statusCode int) error {
	response := NewResponse(statusCode)
	response.SetCSeq(cseq)
	response.SetHeader(HeaderServer, "Sol RTSP Server")
	
	return s.writer.WriteResponse(response)
}

// parseTransport parses the Transport header
func (s *Session) parseTransport(transport string) {
	// Simple parsing for client_port
	if strings.Contains(transport, "client_port=") {
		parts := strings.Split(transport, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "client_port=") {
				portsStr := strings.TrimPrefix(part, "client_port=")
				portParts := strings.Split(portsStr, "-")
				for _, portStr := range portParts {
					if port, err := strconv.Atoi(portStr); err == nil {
						s.clientPorts = append(s.clientPorts, port)
					}
				}
				break
			}
		}
	}
}

// buildTransportResponse builds the Transport response header
func (s *Session) buildTransportResponse() string {
	transport := s.transport
	
	// Add server ports
	if len(s.serverPorts) >= 2 {
		transport += fmt.Sprintf(";server_port=%d-%d", s.serverPorts[0], s.serverPorts[1])
	}
	
	return transport
}

// generateSDP generates a simple SDP for testing
func (s *Session) generateSDP() string {
	return fmt.Sprintf(`v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=Sol RTSP Stream\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:track1\r\n`)
}
