package rtsp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/rtp"
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
	clientPorts     []int // RTP port (UDP only)
	serverPorts     []int // RTP port (UDP only)
	transport       string
	transportMode   TransportMode     // UDP or TCP mode
	interleavedMode bool              // RTP over TCP interleaved
	rtpChannel      int               // RTP channel number for TCP
	rtpSession      *rtp.RTPSession   // RTP session for this RTSP session
	rtpTransport    *rtp.RTPTransport // Reference to RTP transport
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

// TransportMode represents the transport mode (UDP or TCP)
type TransportMode int

const (
	TransportUDP TransportMode = iota
	TransportTCP
)

// combinedReader helps us put back the first byte we peeked
type combinedReader struct {
	firstByte byte
	hasFirst  bool
	reader    io.Reader
}

func (cr *combinedReader) Read(p []byte) (n int, err error) {
	if cr.hasFirst && len(p) > 0 {
		p[0] = cr.firstByte
		cr.hasFirst = false
		if len(p) == 1 {
			return 1, nil
		}
		// Read the rest from the underlying reader
		n2, err2 := cr.reader.Read(p[1:])
		return n2 + 1, err2
	}
	return cr.reader.Read(p)
}

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
func NewSession(conn net.Conn, externalChannel chan interface{}, rtpTransport *rtp.RTPTransport) *Session {
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
		rtpTransport:    rtpTransport,
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

// handleRequests handles incoming RTSP requests and interleaved data
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

		// Peek first byte to determine if it's RTSP request or interleaved data
		firstByte := make([]byte, 1)
		n, err := s.conn.Read(firstByte)
		if err != nil {
			slog.Error("Failed to read from connection", "sessionId", s.sessionId, "err", err)
			return
		}
		if n == 0 {
			continue
		}

		// Check if it's interleaved data (starts with '$')
		if firstByte[0] == '$' {
			if err := s.handleInterleavedData(); err != nil {
				slog.Error("Failed to handle interleaved data", "sessionId", s.sessionId, "err", err)
				return
			}
			continue
		}

		// It's an RTSP request - put the byte back and read normally
		combinedReader := &combinedReader{
			firstByte: firstByte[0],
			hasFirst:  true,
			reader:    s.conn,
		}
		s.reader = NewMessageReader(combinedReader)

		request, err := s.reader.ReadRequest()
		if err != nil {
			slog.Error("Failed to read RTSP request", "sessionId", s.sessionId, "err", err)
			return
		}

		// Reset reader to use original connection
		s.reader = NewMessageReader(s.conn)

		s.lastActivity = time.Now()
		slog.Debug("RTSP request received", "sessionId", s.sessionId, "method", request.Method, "uri", request.URI, "cseq", request.CSeq)

		if err := s.handleRequest(request); err != nil {
			slog.Error("Failed to handle RTSP request", "sessionId", s.sessionId, "method", request.Method, "err", err)
			s.sendErrorResponse(request.CSeq, StatusInternalServerError)
		}
	}
}

// handleInterleavedData handles interleaved RTP/RTCP data
func (s *Session) handleInterleavedData() error {
	// Read the rest of the interleaved frame header
	header := make([]byte, 3) // channel(1) + length(2)
	if _, err := io.ReadFull(s.conn, header); err != nil {
		return fmt.Errorf("failed to read interleaved header: %v", err)
	}

	channel := header[0]
	length := (uint16(header[1]) << 8) | uint16(header[2])

	// Read the data
	data := make([]byte, length)
	if _, err := io.ReadFull(s.conn, data); err != nil {
		return fmt.Errorf("failed to read interleaved data: %v", err)
	}

	s.lastActivity = time.Now()

	// Process the data based on channel
	if int(channel) == s.rtpChannel {
		// RTP data from client
		slog.Debug("Received interleaved RTP data from client", "sessionId", s.sessionId, "dataSize", len(data))
		// Send RTP packet received event
		if s.externalChannel != nil {
			select {
			case s.externalChannel <- RTPPacketReceived{
				SessionId:   s.sessionId,
				StreamPath:  s.streamPath,
				Data:        data,
				Timestamp:   0, // TODO: extract from RTP header
				PayloadType: rtp.PayloadTypeH264,
			}:
			default:
			}
		}
	} else {
		slog.Warn("Received interleaved data on unknown channel", "sessionId", s.sessionId, "channel", channel)
	}

	return nil
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
	// Validate session ID for non-setup requests
	if req.Method != MethodOptions && req.Method != MethodDescribe && req.Method != MethodSetup && req.Method != MethodAnnounce {
		sessionHeader := req.GetHeader(HeaderSession)
		if sessionHeader == "" {
			return s.sendErrorResponse(req.CSeq, StatusSessionNotFound)
		}

		// Extract session ID (remove timeout parameter)
		sessionParts := strings.Split(sessionHeader, ";")
		if len(sessionParts) > 0 && sessionParts[0] != s.sessionId {
			return s.sendErrorResponse(req.CSeq, StatusSessionNotFound)
		}
	}

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
	case MethodGetParam:
		return s.handleGetParameter(req)
	case MethodSetParam:
		return s.handleSetParameter(req)
	default:
		return s.sendErrorResponse(req.CSeq, StatusMethodNotAllowed)
	}
}

// handleOptions handles OPTIONS request
func (s *Session) handleOptions(req *Request) error {
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderPublic, "OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY, PAUSE, ANNOUNCE, RECORD, GET_PARAMETER, SET_PARAMETER")
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

	// Generate more detailed SDP
	sdp := s.generateDetailedSDP()

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

	// Create RTP session based on transport mode
	if s.transportMode == TransportTCP && s.interleavedMode {
		// TCP interleaved mode - no separate UDP session needed
		slog.Info("TCP interleaved mode setup", "sessionId", s.sessionId, "rtpChannel", s.rtpChannel)
	} else if len(s.clientPorts) >= 2 && s.rtpTransport != nil {
		// UDP mode - create RTP session
		ssrc := uint32(0x12345678) // TODO: generate unique SSRC

		// Get client IP from connection
		clientIP := s.conn.RemoteAddr().(*net.TCPAddr).IP.String()

		// Create RTP session
		rtpSession, err := s.rtpTransport.CreateSession(ssrc, rtp.PayloadTypeH264,
			s.clientPorts[0], clientIP)
		if err != nil {
			slog.Error("Failed to create RTP session", "err", err)
			return s.sendErrorResponse(req.CSeq, StatusInternalServerError)
		}

		s.rtpSession = rtpSession
		s.serverPorts = []int{8000, 8001} // TODO: get from RTP transport
		slog.Info("UDP RTP session created", "sessionId", s.sessionId, "ssrc", ssrc)
	} else {
		s.serverPorts = []int{8000, 8001}
	}

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

	// Parse Range header if present
	rangeHeader := req.GetHeader(HeaderRange)
	if rangeHeader != "" {
		slog.Debug("Range header received", "sessionId", s.sessionId, "range", rangeHeader)
		// TODO: implement range support
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

// handleGetParameter handles GET_PARAMETER request
func (s *Session) handleGetParameter(req *Request) error {
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, s.sessionId)

	// Basic keep-alive response
	return s.writer.WriteResponse(response)
}

// handleSetParameter handles SET_PARAMETER request
func (s *Session) handleSetParameter(req *Request) error {
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, s.sessionId)

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
	s.transportMode = TransportUDP // Default to UDP

	// Check for TCP interleaved mode
	if strings.Contains(transport, "RTP/AVP/TCP") {
		s.transportMode = TransportTCP
		s.interleavedMode = true

		// Parse interleaved channels
		if strings.Contains(transport, "interleaved=") {
			parts := strings.Split(transport, ";")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "interleaved=") {
					channelsStr := strings.TrimPrefix(part, "interleaved=")
					channelParts := strings.Split(channelsStr, "-")
					if len(channelParts) >= 2 {
						if rtpCh, err := strconv.Atoi(channelParts[0]); err == nil {
							s.rtpChannel = rtpCh
						}
					} else if len(channelParts) == 1 {
						// Only RTP channel specified
						if rtpCh, err := strconv.Atoi(channelParts[0]); err == nil {
							s.rtpChannel = rtpCh
						}
					}
					break
				}
			}
		} else {
			// Default interleaved channels if not specified
			s.rtpChannel = 0
		}

		slog.Info("TCP interleaved transport", "sessionId", s.sessionId, "rtpChannel", s.rtpChannel)
		return
	}

	// UDP mode - parse client_port
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
		slog.Info("UDP transport", "sessionId", s.sessionId, "clientPorts", s.clientPorts)
	}
}

// buildTransportResponse builds the Transport response header
func (s *Session) buildTransportResponse() string {
	transport := s.transport

	if s.transportMode == TransportTCP && s.interleavedMode {
		// TCP interleaved mode
		transport += fmt.Sprintf(";interleaved=%d", s.rtpChannel)
	} else {
		// UDP mode - add server ports
		if len(s.serverPorts) >= 1 {
			transport += fmt.Sprintf(";server_port=%d", s.serverPorts[0])
		}
	}

	return transport
}

// generateDetailedSDP generates a detailed SDP
func (s *Session) generateDetailedSDP() string {
	return fmt.Sprintf(`v=0\r
o=- %d %d IN IP4 127.0.0.1\r
s=Sol RTSP Stream\r
i=RTSP Server Stream\r
c=IN IP4 0.0.0.0\r
t=0 0\r
a=tool:Sol RTSP Server\r
a=range:npt=0-\r
m=video 0 RTP/AVP 96\r
c=IN IP4 0.0.0.0\r
b=AS:500\r
a=rtpmap:96 H264/90000\r
a=fmtp:96 packetization-mode=1;sprop-parameter-sets=Z0LAHpWgUH5PIAEAAAMAEAAAAwPA8UKZYA==,aMuBcsg=\r
a=control:track1\r
m=audio 0 RTP/AVP 97\r
c=IN IP4 0.0.0.0\r
b=AS:128\r
a=rtpmap:97 MPEG4-GENERIC/48000/2\r
a=fmtp:97 streamtype=5;profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3;config=119056E500\r
a=control:track2\r
`, time.Now().Unix(), time.Now().Unix())
}

// SendInterleavedRTPPacket sends RTP packet over TCP interleaved
func (s *Session) SendInterleavedRTPPacket(data []byte) error {
	if s.transportMode != TransportTCP || !s.interleavedMode {
		return fmt.Errorf("session is not in TCP interleaved mode")
	}

	// Interleaved frame format:
	// '$' + channel + length(2 bytes) + data
	frame := make([]byte, 4+len(data))
	frame[0] = '$'                    // Magic byte
	frame[1] = byte(s.rtpChannel)     // Channel number
	frame[2] = byte(len(data) >> 8)   // Length high byte
	frame[3] = byte(len(data) & 0xFF) // Length low byte
	copy(frame[4:], data)             // RTP packet data

	_, err := s.conn.Write(frame)
	if err != nil {
		return fmt.Errorf("failed to send interleaved RTP packet: %v", err)
	}

	slog.Debug("Interleaved RTP packet sent", "sessionId", s.sessionId, "channel", s.rtpChannel, "dataSize", len(data))
	return nil
}

// IsUDPMode returns true if session is using UDP transport
func (s *Session) IsUDPMode() bool {
	return s.transportMode == TransportUDP
}

// IsTCPMode returns true if session is using TCP transport
func (s *Session) IsTCPMode() bool {
	return s.transportMode == TransportTCP
}

// IsInterleavedMode returns true if session is using TCP interleaved mode
func (s *Session) IsInterleavedMode() bool {
	return s.transportMode == TransportTCP && s.interleavedMode
}
