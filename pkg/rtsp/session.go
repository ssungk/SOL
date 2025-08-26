package rtsp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/core"
	"sol/pkg/rtp"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// Session represents an RTSP client session
type Session struct {
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
	rtpSequence     uint16            // RTP sequence number for packet generation
	timeout         time.Duration
	lastActivity    time.Time
	externalChannel chan<- any
	ctx             context.Context
	cancel          context.CancelFunc
	server          *Server // 서버 참조
	
	// Stream integration (MediaServer에서 설정)
	Stream *core.Stream // 연결된 스트림
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
func NewSession(conn net.Conn, externalChannel chan<- any, rtpTransport *rtp.RTPTransport, server *Server) *Session {
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
		server:          server,
		ctx:             ctx,
		cancel:          cancel,
	}

	return session
}

// Close closes the session - MediaNode 인터페이스 구현
func (s *Session) Close() error {
	slog.Info("RTSP session closing", "sessionId", s.ID())

	// Cancel context
	s.cancel()

	// Close RTP session if exists
	if s.rtpSession != nil && s.rtpTransport != nil {
		s.rtpTransport.RemoveSession(s.rtpSession.GetSSRC())
		s.rtpSession = nil
	}

	// Close connection
	if s.conn != nil {
		s.conn.Close()
	}

	// Remove from server
	if s.server != nil {
		s.server.removeSession(s.ID())
	}

	// Send node termination event to MediaServer
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- core.NodeTerminated{
			ID: s.ID(),
		}:
		default:
		}
	}
	
	return nil
}

// handleRequests handles incoming RTSP requests and interleaved data
func (s *Session) handleRequests() {
	defer s.Close()

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
			slog.Error("Failed to read from connection", "sessionId", s.ID(), "err", err)
			return
		}
		if n == 0 {
			continue
		}

		// Check if it's interleaved data (starts with '$')
		if firstByte[0] == '$' {
			if err := s.handleInterleavedData(); err != nil {
				slog.Error("Failed to handle interleaved data", "sessionId", s.ID(), "err", err)
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
			slog.Error("Failed to read RTSP request", "sessionId", s.ID(), "err", err)
			return
		}

		// Reset reader to use original connection
		s.reader = NewMessageReader(s.conn)

		s.lastActivity = time.Now()

		if err := s.handleRequest(request); err != nil {
			slog.Error("Failed to handle RTSP request", "sessionId", s.ID(), "method", request.Method, "err", err)
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
		// RTP data from client - convert to media frame and send to stream
		
		// RTP 패킷을 core.Packet으로 변환
		packet, err := s.convertRTPToPacket(data, s.streamPath)
		if err != nil {
			slog.Error("Failed to convert RTP to packet", "sessionId", s.ID(), "err", err)
			return nil
		}

		// Stream에 패킷 전송 (RECORD 모드에서)
		if s.state == StateRecording && s.Stream != nil {
			// 트랙이 없으면 추가
			if s.Stream.TrackCount() <= packet.TrackIndex {
				s.Stream.AddTrack(packet.Codec, core.TimeScaleRTP_Video)
			}
			
			s.Stream.SendPacket(packet)
		}
	} else {
		slog.Warn("Received interleaved data on unknown channel", "sessionId", s.ID(), "channel", channel)
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
				slog.Info("RTSP session timed out", "sessionId", s.ID(), "lastActivity", s.lastActivity, "timeout", s.timeout)
				s.Close()
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
		if len(sessionParts) > 0 && sessionParts[0] != fmt.Sprintf("%d", s.ID()) {
			return s.sendErrorResponse(req.CSeq, StatusSessionNotFound)
		}
	}

	switch req.Method {
	case MethodOptions:
		return s.handleOptions(req)
	case MethodDescribe:
		return s.handleDescribe(req)
	case MethodAnnounce:
		return s.handleAnnounce(req)
	case MethodSetup:
		return s.handleSetup(req)
	case MethodRecord:
		return s.handleRecord(req)
	case MethodPlay:
		return s.handlePlay(req)
	case MethodPause:
		return s.handlePause(req)
	case MethodTeardown:
		return s.handleTeardown(req)
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
	// URI에서 경로 부분만 추출 (rtsp://host:port/path -> path)
	s.streamPath = s.extractStreamPath(req.URI)

	// DESCRIBE 요청은 단순히 SDP 정보를 반환하므로 별도 이벤트 불필요
	slog.Info("DESCRIBE request processed", "sessionId", s.ID(), "streamPath", s.streamPath)

	// Generate more detailed SDP
	sdp := s.generateDetailedSDP()

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderContentType, "application/sdp")
	response.SetHeader(HeaderContentLength, strconv.Itoa(len(sdp)))
	response.Body = []byte(sdp)

	return s.writer.WriteResponse(response)
}

// handleAnnounce handles ANNOUNCE request
func (s *Session) handleAnnounce(req *Request) error {
	s.streamPath = s.extractStreamPath(req.URI)

	// ANNOUNCE는 SDP 정보 설정이므로 메타데이터로 처리
	if s.externalChannel != nil {
		// SDP 정보를 메타데이터로 변환하여 저장
		sdp := string(req.Body)
		slog.Info("ANNOUNCE received with SDP", "sessionId", s.ID(), "streamPath", s.streamPath, "sdpLength", len(sdp))
		// SDP 정보는 현재 로그로만 기록하고 향후 필요시 메타데이터로 변환 예정
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)

	return s.writer.WriteResponse(response)
}

// handleSetup handles SETUP request
func (s *Session) handleSetup(req *Request) error {
	if s.state != StateInit && s.state != StateReady {
		slog.Warn("SETUP request in invalid state", "sessionId", s.ID(), "currentState", s.state.String())
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}
	
	// Parse transport header
	transportHeader := req.GetHeader(HeaderTransport)
	if transportHeader == "" {
		slog.Error("Missing Transport header in SETUP request", "sessionId", s.ID())
		return s.sendErrorResponse(req.CSeq, StatusBadRequest)
	}

	s.transport = transportHeader
	s.parseTransport(transportHeader)
	
	// 스트림 경로 설정 (URI에서 track 정보 제거)
	if s.streamPath == "" {
		uri := strings.TrimSuffix(req.URI, "/track0")
		uri = strings.TrimSuffix(uri, "/track1")
		s.streamPath = s.extractStreamPath(uri)
		slog.Info("Stream path set from SETUP", "sessionId", s.ID(), "streamPath", s.streamPath)
	}

	// Create RTP session based on transport mode
	if s.transportMode == TransportTCP && s.interleavedMode {
		// TCP interleaved mode - no separate UDP session needed
		slog.Info("TCP interleaved mode setup", "sessionId", s.ID(), "rtpChannel", s.rtpChannel)
	} else if len(s.clientPorts) >= 1 && s.rtpTransport != nil {
		// UDP mode - create RTP session
		ssrc := uint32(s.ID()) // 세션 ID를 SSRC로 사용

		// Get client IP from connection
		clientAddr := s.conn.RemoteAddr()
		var clientIP string
		if tcpAddr, ok := clientAddr.(*net.TCPAddr); ok {
			clientIP = tcpAddr.IP.String()
		} else {
			slog.Error("Failed to get client IP", "sessionId", s.ID(), "addr", clientAddr)
			return s.sendErrorResponse(req.CSeq, StatusInternalServerError)
		}

		// Create RTP session
		rtpSession, err := s.rtpTransport.CreateSession(ssrc, rtp.PayloadTypeH264,
			s.clientPorts[0], clientIP)
		if err != nil {
			slog.Error("Failed to create RTP session", "sessionId", s.ID(), "err", err)
			return s.sendErrorResponse(req.CSeq, StatusInternalServerError)
		}

		s.rtpSession = rtpSession
		s.serverPorts = []int{8000, 8001} // 고정 포트 사용 (향후 동적 할당 가능)
		slog.Info("UDP RTP session created", "sessionId", s.ID(), "ssrc", ssrc, "clientIP", clientIP, "clientPort", s.clientPorts[0])
	} else {
		// Transport 모드가 명확하지 않은 경우 기본 설정
		s.serverPorts = []int{8000, 8001}
		slog.Info("Default server ports set", "sessionId", s.ID(), "ports", s.serverPorts)
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderTransport, s.buildTransportResponse())
	response.SetHeader(HeaderSession, fmt.Sprintf("%d;timeout=%d", s.ID(), int(s.timeout.Seconds())))

	s.state = StateReady
	slog.Info("SETUP completed", "sessionId", s.ID(), "transport", s.transportMode, "streamPath", s.streamPath)

	return s.writer.WriteResponse(response)
}

// handleRecord handles RECORD request
func (s *Session) handleRecord(req *Request) error {
	if s.state != StateReady {
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}

	// StreamPath collision detection
	streamPath := s.streamPath

	// 1단계: 스트림 준비 (MediaServer에서 처리하므로 nil로 전달)
	stream := (*core.Stream)(nil) // RTSP는 MediaServer에서 스트림 생성

	// 2단계: MediaServer에 실제 record 시도 (collision detection + 원자적 점유)
	if !s.attemptStreamPublish(streamPath, stream) {
		return s.sendRecordErrorResponse(req.CSeq, "Stream path was taken by another client")
	}

	// 3단계: 성공 응답 전송 (MediaServer 등록은 attemptStreamPublish에서 완료됨)
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))

	s.state = StateRecording

	return s.writer.WriteResponse(response)
}

// handlePlay handles PLAY request
func (s *Session) handlePlay(req *Request) error {
	if s.state != StateReady {
		slog.Warn("PLAY request in invalid state", "sessionId", s.ID(), "currentState", s.state.String())
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}
	
	if s.streamPath == "" {
		slog.Error("No stream path set for PLAY request", "sessionId", s.ID())
		return s.sendErrorResponse(req.CSeq, StatusBadRequest)
	}

	// Parse Range header if present
	rangeHeader := req.GetHeader(HeaderRange)
	if rangeHeader != "" {
		slog.Info("Range header received", "sessionId", s.ID(), "range", rangeHeader)
		// Range 헤더 파싱 (향후 seek 기능 지원 시 사용)
	}

	// Send play started event to MediaServer
	if s.externalChannel != nil {
		responseChan := make(chan core.Response, 1)
		
		// RTSP가 지원하는 코덱 목록
		supportedCodecs := []core.Codec{core.H264, core.H265, core.AAC, core.Opus}
		
		select {
		case s.externalChannel <- core.NewSubscribeStarted(s.ID(), s.streamPath, supportedCodecs, responseChan):
			// 응답 대기 (타임아웃 5초)
			select {
			case response := <-responseChan:
				if !response.Success {
					slog.Error("Subscribe failed", "sessionId", s.ID(), "streamPath", s.streamPath, "error", response.Error)
					if response.Error == "stream not found" {
						return s.sendErrorResponse(req.CSeq, StatusNotFound)
					}
					return s.sendErrorResponse(req.CSeq, StatusInternalServerError)
				}
				slog.Info("Subscribe successful", "sessionId", s.ID(), "streamPath", s.streamPath)
			case <-time.After(5 * time.Second):
				slog.Error("Subscribe timeout", "sessionId", s.ID(), "streamPath", s.streamPath)
				return s.sendErrorResponse(req.CSeq, StatusRequestTimeout)
			case <-s.ctx.Done():
				slog.Error("Subscribe cancelled", "sessionId", s.ID(), "streamPath", s.streamPath)
				return s.sendErrorResponse(req.CSeq, StatusRequestTimeout)
			}
		default:
			slog.Error("Failed to send SubscribeStarted event - channel full", "sessionId", s.ID())
			return s.sendErrorResponse(req.CSeq, StatusServiceUnavailable)
		}
	}

	// RTP 시퀀스 넘버 초기화
	s.rtpSequence = 1

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d;timeout=%d", s.ID(), int(s.timeout.Seconds())))
	response.SetHeader(HeaderRTPInfo, fmt.Sprintf("url=%s;seq=%d;rtptime=0", req.URI, s.rtpSequence))

	s.state = StatePlaying
	slog.Info("PLAY started", "sessionId", s.ID(), "streamPath", s.streamPath, "transport", s.transportMode)

	return s.writer.WriteResponse(response)
}

// handlePause handles PAUSE request
func (s *Session) handlePause(req *Request) error {
	if s.state != StatePlaying {
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}

	// Send play stopped event to MediaServer
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- core.SubscribeStopped{
			ID:       s.ID(),
			StreamID: s.streamPath,
		}:
		default:
		}
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))

	s.state = StateReady

	return s.writer.WriteResponse(response)
}

// handleTeardown handles TEARDOWN request
func (s *Session) handleTeardown(req *Request) error {
	// Send play stopped event to MediaServer
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- core.SubscribeStopped{
			ID:       s.ID(),
			StreamID: s.streamPath,
		}:
		default:
		}
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))

	s.state = StateInit

	// Schedule session termination after response
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Close()
	}()

	return s.writer.WriteResponse(response)
}

// handleGetParameter handles GET_PARAMETER request
func (s *Session) handleGetParameter(req *Request) error {
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))

	// Basic keep-alive response
	return s.writer.WriteResponse(response)
}

// handleSetParameter handles SET_PARAMETER request
func (s *Session) handleSetParameter(req *Request) error {
	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))

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

		slog.Info("TCP interleaved transport", "sessionId", s.ID(), "rtpChannel", s.rtpChannel)
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
		slog.Info("UDP transport", "sessionId", s.ID(), "clientPorts", s.clientPorts)
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

// generateDetailedSDP generates a detailed SDP based on stream information
func (s *Session) generateDetailedSDP() string {
	sessionID := time.Now().Unix()
	version := sessionID
	
	// Get server IP from connection
	serverIP := "0.0.0.0" // Any IP
	if s.conn != nil {
		if localAddr := s.conn.LocalAddr(); localAddr != nil {
			if tcpAddr, ok := localAddr.(*net.TCPAddr); ok {
				serverIP = tcpAddr.IP.String()
			}
		}
	}
	
	sdp := fmt.Sprintf("v=0\r\n")
	sdp += fmt.Sprintf("o=- %d %d IN IP4 %s\r\n", sessionID, version, serverIP)
	sdp += fmt.Sprintf("s=Sol RTSP Stream\r\n")
	sdp += fmt.Sprintf("i=Live Stream from Sol Server\r\n")
	sdp += fmt.Sprintf("c=IN IP4 %s\r\n", serverIP)
	sdp += fmt.Sprintf("t=0 0\r\n")
	sdp += fmt.Sprintf("a=tool:Sol RTSP Server v1.0\r\n")
	sdp += fmt.Sprintf("a=type:broadcast\r\n")
	sdp += fmt.Sprintf("a=control:*\r\n")
	sdp += fmt.Sprintf("a=range:npt=0-\r\n")
	
	// Video track (H.264) - Default SDP
	sdp += fmt.Sprintf("m=video 0 RTP/AVP 96\r\n")
	sdp += fmt.Sprintf("c=IN IP4 %s\r\n", serverIP)
	sdp += fmt.Sprintf("b=AS:5000\r\n") // 5Mbps bitrate
	sdp += fmt.Sprintf("a=rtpmap:96 H264/90000\r\n")
	sdp += fmt.Sprintf("a=fmtp:96 packetization-mode=1;profile-level-id=42e01e;sprop-parameter-sets=Z0LAHtkDxWhAAAADAEAAAAwDxYuS,aMuMsg==\r\n")
	sdp += fmt.Sprintf("a=control:track0\r\n")
	
	// Audio track (AAC) - Default SDP
	sdp += fmt.Sprintf("m=audio 0 RTP/AVP 97\r\n")
	sdp += fmt.Sprintf("c=IN IP4 %s\r\n", serverIP)
	sdp += fmt.Sprintf("b=AS:128\r\n") // 128kbps bitrate
	sdp += fmt.Sprintf("a=rtpmap:97 MPEG4-GENERIC/48000/2\r\n")
	sdp += fmt.Sprintf("a=fmtp:97 streamtype=5;profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3;config=119056E500\r\n")
	sdp += fmt.Sprintf("a=control:track1\r\n")
	
	return sdp
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

// MediaNode 인터페이스 구현

// ID MediaNode 인터페이스 구현 - 노드 고유 ID 반환
func (s *Session) ID() uintptr {
	return uintptr(unsafe.Pointer(s))
}

// NodeType MediaNode 인터페이스 구현 - 노드 타입 반환
func (s *Session) NodeType() core.NodeType {
	return core.NodeTypeRTSP
}

// Address MediaNode 인터페이스 구현 - 주소 반환
func (s *Session) Address() string {
	if s.conn != nil {
		return s.conn.RemoteAddr().String()
	}
	return ""
}

// MediaSink 인터페이스 구현 (RTSP 플레이어 세션용)

// SendPacket MediaSink 인터페이스 구현 - 패킷을 세션으로 전송
func (s *Session) SendPacket(streamID string, packet core.Packet) error {
	// RTSP 세션이 플레이 중이고 스트림 ID가 일치하는 경우에만 전송
	if s.state != StatePlaying || s.streamPath != streamID {
		return fmt.Errorf("session not ready for packet: state=%v, streamPath=%s", s.state, s.streamPath)
	}

	// Packet을 RTP 패킷으로 변환
	rtpData, err := s.convertPacketToRTP(packet)
	if err != nil {
		return fmt.Errorf("failed to convert packet to RTP: %v", err)
	}

	// 전송 모드에 따라 RTP 패킷 전송
	if s.IsInterleavedMode() {
		// TCP interleaved mode
		return s.SendInterleavedRTPPacket(rtpData)
	} else if s.IsUDPMode() && s.rtpSession != nil && s.rtpTransport != nil {
		// UDP mode
		return s.rtpTransport.SendRTPPacket(s.rtpSession.GetSSRC(), rtpData, 0, false)
	}

	return fmt.Errorf("no valid transport setup for session")
}

// SendMetadata MediaSink 인터페이스 구현 - 메타데이터를 세션으로 전송
func (s *Session) SendMetadata(streamID string, metadata map[string]string) error {
	// RTSP에서는 SDP를 통해 메타데이터가 전송되므로 현재는 처리하지 않음
	return nil
}

// convertPacketToRTP Packet을 RTP 패킷으로 변환
func (s *Session) convertPacketToRTP(packet core.Packet) ([]byte, error) {
	if len(packet.Data) == 0 {
		return nil, fmt.Errorf("empty packet data")
	}
	
	// 모든 버퍼를 결합하여 전체 페이로드 구성
	var totalSize int
	for _, buffer := range packet.Data {
		if buffer != nil {
			totalSize += len(buffer.Data())
		}
	}
	
	if totalSize == 0 {
		return nil, fmt.Errorf("empty payload data")
	}
	
	payload := make([]byte, totalSize)
	offset := 0
	for _, buffer := range packet.Data {
		if buffer != nil && len(buffer.Data()) > 0 {
			copy(payload[offset:], buffer.Data())
			offset += len(buffer.Data())
		}
	}
	
	// 시퀀스 넘버 증가
	s.rtpSequence++
	
	// RTP 헤더 생성
	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80 // V=2, P=0, X=0, CC=0
	
	// 마커 비트와 페이로드 타입 설정
	var payloadType uint8
	var marker bool
	
	if packet.IsVideo() {
		payloadType = 96 // H.264
		// 마지막 NAL 단위에 마커 비트 설정
		marker = true // 일단 모든 비디오 패킷에 마커 설정 (단일 NAL 가정)
	} else if packet.IsAudio() {
		payloadType = 97 // AAC
		marker = true    // 오디오 패킷은 항상 마커 비트 설정
	} else {
		return nil, fmt.Errorf("unsupported packet type")
	}
	
	if marker {
		rtpHeader[1] = 0x80 | payloadType // M=1
	} else {
		rtpHeader[1] = payloadType // M=0
	}
	
	// 시퀀스 넘버 (16비트, 빅엔디안)
	rtpHeader[2] = byte(s.rtpSequence >> 8)
	rtpHeader[3] = byte(s.rtpSequence & 0xFF)
	
	// 타임스탬프 (32비트, 빅엔디안)
	var timestamp uint32
	if packet.IsVideo() {
		timestamp = uint32(packet.DTS * 90) // ms to 90kHz clock
	} else {
		timestamp = uint32(packet.DTS * 48) // ms to 48kHz clock
	}
	
	rtpHeader[4] = byte(timestamp >> 24)
	rtpHeader[5] = byte(timestamp >> 16)
	rtpHeader[6] = byte(timestamp >> 8)
	rtpHeader[7] = byte(timestamp)
	
	// SSRC (세션별 고유 ID)
	ssrc := uint32(s.ID())
	rtpHeader[8] = byte(ssrc >> 24)
	rtpHeader[9] = byte(ssrc >> 16)
	rtpHeader[10] = byte(ssrc >> 8)
	rtpHeader[11] = byte(ssrc)

	// H.264 비디오의 경우 NAL Unit 처리
	if packet.IsVideo() && packet.Codec == core.H264 {
		return s.createH264RTPPacket(rtpHeader, payload)
	}

	// RTP 패킷 = 헤더 + 페이로드 (기본값)
	rtpPacket := make([]byte, len(rtpHeader)+len(payload))
	copy(rtpPacket, rtpHeader)
	copy(rtpPacket[len(rtpHeader):], payload)
	
	return rtpPacket, nil
}

// createH264RTPPacket H.264 NAL Unit을 RTP 패킷으로 변환
func (s *Session) createH264RTPPacket(rtpHeader []byte, payload []byte) ([]byte, error) {
	// H.264 Annex-B 포맷에서 start code 제거 후 NAL unit 추출
	if len(payload) < 4 {
		return nil, fmt.Errorf("H.264 payload too short")
	}
	
	nalUnits := s.extractNALUnits(payload)
	if len(nalUnits) == 0 {
		return nil, fmt.Errorf("no NAL units found")
	}
	
	// Single NAL Unit Mode (RFC 6184)
	if len(nalUnits) == 1 && len(nalUnits[0]) <= 1460 { // MTU 고려
		rtpPacket := make([]byte, len(rtpHeader)+len(nalUnits[0]))
		copy(rtpPacket, rtpHeader)
		copy(rtpPacket[len(rtpHeader):], nalUnits[0])
		return rtpPacket, nil
	}
	
	// Multiple NAL Units나 Fragmentation이 필요한 경우는 Single NAL로 처리
	// 첫 번째 NAL Unit 사용
	rtpPacket := make([]byte, len(rtpHeader)+len(nalUnits[0]))
	copy(rtpPacket, rtpHeader)
	copy(rtpPacket[len(rtpHeader):], nalUnits[0])
	
	return rtpPacket, nil
}

// extractNALUnits Annex-B 포맷에서 NAL Unit들을 추출
func (s *Session) extractNALUnits(data []byte) [][]byte {
	var nalUnits [][]byte
	i := 0
	
	for i < len(data) {
		// Start code 찾기 (0x000001 또는 0x00000001)
		startCodeLen := 0
		if i+2 < len(data) && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			startCodeLen = 3
		} else if i+3 < len(data) && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			startCodeLen = 4
		} else {
			i++
			continue
		}
		
		nalStart := i + startCodeLen
		nalEnd := len(data) // 기본값: 데이터 끝까지
		
		// 다음 start code 찾기
		for j := nalStart + 1; j < len(data)-2; j++ {
			if (data[j] == 0x00 && data[j+1] == 0x00 && data[j+2] == 0x01) ||
				(j < len(data)-3 && data[j] == 0x00 && data[j+1] == 0x00 && data[j+2] == 0x00 && data[j+3] == 0x01) {
				nalEnd = j
				break
			}
		}
		
		if nalEnd > nalStart {
			nalUnit := data[nalStart:nalEnd]
			nalUnits = append(nalUnits, nalUnit)
		}
		
		i = nalEnd
	}
	
	return nalUnits
}

// GetStreamPath 현재 세션의 스트림 경로 반환
func (s *Session) GetStreamPath() string {
	return s.streamPath
}

// extractStreamPath URI에서 스트림 경로 부분 추출
func (s *Session) extractStreamPath(uri string) string {
	// rtsp://host:port/path -> path
	if strings.HasPrefix(uri, "rtsp://") {
		// "rtsp://" 부분 제거
		uri = strings.TrimPrefix(uri, "rtsp://")
		
		// host:port 부분 찾아서 제거
		slashIndex := strings.Index(uri, "/")
		if slashIndex >= 0 {
			return uri[slashIndex+1:] // "/" 다음 부분 반환
		}
	}
	
	// RTSP URI가 아니거나 "/" 없으면 전체 URI 반환
	return uri
}

// MediaSource 인터페이스 구현 (RECORD 시 사용)
// RTSP Session은 RECORD 모드에서는 MediaSource로, PLAY 모드에서는 MediaSink로 동작

// convertRTPToPacket RTP 패킷을 Packet으로 변환 (RECORD 시 사용)
func (s *Session) convertRTPToPacket(rtpData []byte, streamID string) (core.Packet, error) {
	if len(rtpData) < 12 {
		return core.Packet{}, fmt.Errorf("RTP packet too short: %d bytes", len(rtpData))
	}

	// RTP 헤더 파싱
	version := (rtpData[0] & 0xC0) >> 6
	if version != 2 {
		return core.Packet{}, fmt.Errorf("unsupported RTP version: %d", version)
	}

	payloadType := rtpData[1] & 0x7F
	timestamp := uint32(rtpData[4])<<24 | uint32(rtpData[5])<<16 | uint32(rtpData[6])<<8 | uint32(rtpData[7])

	// 페이로드 추출 (헤더 제외)
	payload := rtpData[12:]

	// 페이로드 타입에 따른 Codec, BitstreamFormat, PacketType 결정
	var codec core.Codec
	var format core.BitstreamFormat
	var packetType core.PacketType
	var trackIndex int

	switch payloadType {
	case 96: // H.264
		codec = core.H264
		format = core.FormatH26xAnnexB // RTSP는 Annex-B 포맷 사용
		packetType = core.TypeData    // 기본값으로 Data
		trackIndex = 0 // 비디오는 트랙 0
	case 97: // AAC
		codec = core.AAC
		format = core.FormatRawStream // 오디오는 raw 데이터
		packetType = core.TypeData // 기본값으로 Data
		trackIndex = 1 // 오디오는 트랙 1
	default:
		codec = core.H264
		format = core.FormatH26xAnnexB
		packetType = core.TypeData
		trackIndex = 0 // 기본값은 비디오
	}
	
	// 페이로드를 Buffer로 변환
	payloadBuffer := core.NewBuffer(len(payload))
	copy(payloadBuffer.Data(), payload)
	
	packet := core.NewPacket(
		trackIndex,
		codec,
		format,
		packetType,
		uint64(timestamp),
		0, // CTS = 0 (RTSP는 일반적으로 CTS 사용 안함)
		[]*core.Buffer{payloadBuffer},
	)


	return packet, nil
}

// --- MediaSource 인터페이스 구현 (RECORD 모드) ---

// PublishingStreams MediaSource 인터페이스 구현 - 발행 중인 스트림 목록 반환 (RECORD 모드)
func (s *Session) PublishingStreams() []*core.Stream {
	if s.state == StateRecording && s.Stream != nil {
		return []*core.Stream{s.Stream}
	}
	return nil
}

// --- MediaSink 인터페이스 구현 (PLAY 모드) ---

// SubscribedStreams MediaSink 인터페이스 구현 - 구독 중인 스트림 ID 목록 반환 (PLAY 모드)
func (s *Session) SubscribedStreams() []string {
	if s.state == StatePlaying && s.streamPath != "" {
		return []string{s.streamPath}
	}
	return nil
}

// StreamPath collision detection 헬퍼 메서드

// attemptStreamPublish 실제 publish 시도 (collision detection + 원자적 점유)
func (s *Session) attemptStreamPublish(streamPath string, stream *core.Stream) bool {
	if s.externalChannel == nil {
		return true
	}

	responseChan := make(chan core.Response, 1)
	
	request := core.NewPublishStarted(
		s.ID(),
		stream,
		responseChan,
	)

	// 요청 전송
	select {
	case s.externalChannel <- request:
		// 응답 대기 (타임아웃 5초)
		select {
		case response := <-responseChan:
			if response.Error != "" {
				slog.Error("Stream publish attempt failed", "sessionId", s.ID(), "streamPath", streamPath, "error", response.Error)
			}
			return response.Success
		case <-time.After(5 * time.Second):
			slog.Warn("Stream publish attempt timeout", "sessionId", s.ID(), "streamPath", streamPath)
			return false
		case <-s.ctx.Done():
			return false
		}
	case <-s.ctx.Done():
		return false
	default:
		slog.Warn("Failed to send stream publish attempt request - channel full", "sessionId", s.ID(), "streamPath", streamPath)
		return false
	}
}

// sendRecordErrorResponse RTSP RECORD 실패 응답 전송
func (s *Session) sendRecordErrorResponse(cseq int, message string) error {
	slog.Info("RECORD failed", "sessionId", s.ID(), "reason", message)
	
	response := NewResponse(StatusConflict) // 409 Conflict
	response.SetCSeq(cseq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))
	// RTSP에서는 body에 에러 메시지 추가
	response.Body = []byte(message)
	response.SetHeader("Content-Length", fmt.Sprintf("%d", len(response.Body)))
	
	return s.writer.WriteResponse(response)
}

