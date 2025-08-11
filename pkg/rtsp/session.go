package rtsp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/media"
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
	timeout         time.Duration
	lastActivity    time.Time
	externalChannel chan<- any
	ctx             context.Context
	cancel          context.CancelFunc
	
	// Stream integration (MediaServer에서 설정)
	Stream *media.Stream // 연결된 스트림
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
func NewSession(conn net.Conn, externalChannel chan<- any, rtpTransport *rtp.RTPTransport) *Session {
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

	return session
}

// Close closes the session - MediaNode 인터페이스 구현
func (s *Session) Close() error {
	slog.Info("RTSP session closing", "sessionId", s.ID())

	// Cancel context
	s.cancel()

	// Close connection
	if s.conn != nil {
		s.conn.Close()
	}

	// Send node termination event to MediaServer
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- media.NodeTerminated{
			BaseNodeEvent: media.BaseNodeEvent{
				ID:       s.ID(),
				NodeType: s.NodeType(),
			},
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
		slog.Debug("RTSP request received", "sessionId", s.ID(), "method", request.Method, "uri", request.URI, "cseq", request.CSeq)

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
		slog.Debug("Received interleaved RTP data from client", "sessionId", s.ID(), "dataSize", len(data))
		
		// RTP 패킷을 media.Frame으로 변환
		frame, err := s.convertRTPToFrame(data, s.streamPath)
		if err != nil {
			slog.Error("Failed to convert RTP to frame", "sessionId", s.ID(), "err", err)
			return nil
		}

		// Stream에 프레임 전송 (RECORD 모드에서)
		if s.state == StateRecording && s.Stream != nil {
			s.Stream.SendFrame(frame)
			slog.Debug("RTP frame sent to stream", 
				"sessionId", s.ID(), 
				"streamPath", s.streamPath, 
				"subType", frame.SubType,
				"mediaType", frame.Type)
		} else {
			slog.Debug("RTP frame converted but not sent (no stream or not recording)", 
				"sessionId", s.ID(), 
				"streamPath", s.streamPath, 
				"state", s.state.String(),
				"hasStream", s.Stream != nil)
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
				slog.Info("RTSP session timed out", "sessionId", s.ID())
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
		slog.Info("TCP interleaved mode setup", "sessionId", s.ID(), "rtpChannel", s.rtpChannel)
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
		slog.Info("UDP RTP session created", "sessionId", s.ID(), "ssrc", ssrc)
	} else {
		s.serverPorts = []int{8000, 8001}
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderTransport, s.buildTransportResponse())
	response.SetHeader(HeaderSession, fmt.Sprintf("%d;timeout=%d", s.ID(), int(s.timeout.Seconds())))

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
		slog.Debug("Range header received", "sessionId", s.ID(), "range", rangeHeader)
		// TODO: implement range support
	}

	// Send play started event to MediaServer
	if s.externalChannel != nil {
		select {
		case s.externalChannel <- media.PlayStarted{
			BaseNodeEvent: media.BaseNodeEvent{
				ID:       s.ID(),
				NodeType: s.NodeType(),
			},
			StreamId: s.streamPath,
		}:
		default:
		}
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))
	response.SetHeader(HeaderRTPInfo, fmt.Sprintf("url=%s;seq=0;rtptime=0", req.URI))

	s.state = StatePlaying

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
		case s.externalChannel <- media.PlayStopped{
			BaseNodeEvent: media.BaseNodeEvent{
				ID:       s.ID(),
				NodeType: s.NodeType(),
			},
			StreamId: s.streamPath,
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
		case s.externalChannel <- media.PlayStopped{
			BaseNodeEvent: media.BaseNodeEvent{
				ID:       s.ID(),
				NodeType: s.NodeType(),
			},
			StreamId: s.streamPath,
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

// handleRecord handles RECORD request
func (s *Session) handleRecord(req *Request) error {
	if s.state != StateReady {
		return s.sendErrorResponse(req.CSeq, StatusMethodNotValidInThisState)
	}

	// Send publish started event to MediaServer (RECORD = publish)
	if s.externalChannel != nil {
		// RTSP RECORD는 PublishStarted 이벤트로 처리 (스트림 생성은 MediaServer에서)
		select {
		case s.externalChannel <- media.PublishStarted{
			BaseNodeEvent: media.BaseNodeEvent{
				ID:       s.ID(),
				NodeType: s.NodeType(),
			},
			Stream: nil, // MediaServer에서 스트림 생성 및 연결
		}:
			slog.Info("Publish started event sent for RTSP record", "sessionId", s.ID(), "streamPath", s.streamPath)
		default:
			slog.Warn("Failed to send publish started event", "sessionId", s.ID())
		}
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)
	response.SetHeader(HeaderSession, fmt.Sprintf("%d", s.ID()))

	s.state = StateRecording

	return s.writer.WriteResponse(response)
}

// handleAnnounce handles ANNOUNCE request
func (s *Session) handleAnnounce(req *Request) error {
	s.streamPath = req.URI

	// ANNOUNCE는 SDP 정보 설정이므로 메타데이터로 처리
	if s.externalChannel != nil {
		// SDP 정보를 메타데이터로 변환하여 저장
		sdp := string(req.Body)
		slog.Info("ANNOUNCE received with SDP", "sessionId", s.ID(), "streamPath", s.streamPath, "sdpLength", len(sdp))
		// TODO: SDP를 메타데이터로 변환하여 stream에 전송
	}

	response := NewResponse(StatusOK)
	response.SetCSeq(req.CSeq)

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

	slog.Debug("Interleaved RTP packet sent", "sessionId", s.ID(), "channel", s.rtpChannel, "dataSize", len(data))
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
func (s *Session) NodeType() media.NodeType {
	return media.NodeTypeRTSP
}

// Address MediaNode 인터페이스 구현 - 주소 반환
func (s *Session) Address() string {
	if s.conn != nil {
		return s.conn.RemoteAddr().String()
	}
	return ""
}

// MediaSink 인터페이스 구현 (RTSP 플레이어 세션용)

// SendMediaFrame MediaSink 인터페이스 구현 - 미디어 프레임을 세션으로 전송
func (s *Session) SendMediaFrame(streamId string, frame media.Frame) error {
	// RTSP 세션이 플레이 중이고 스트림 ID가 일치하는 경우에만 전송
	if s.state != StatePlaying || s.streamPath != streamId {
		return fmt.Errorf("session not ready for frame: state=%v, streamPath=%s", s.state, s.streamPath)
	}

	// media.Frame을 RTP 패킷으로 변환
	rtpData, err := s.convertFrameToRTP(frame)
	if err != nil {
		return fmt.Errorf("failed to convert frame to RTP: %v", err)
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
func (s *Session) SendMetadata(streamId string, metadata map[string]string) error {
	// RTSP에서는 SDP를 통해 메타데이터가 전송되므로 현재는 처리하지 않음
	slog.Debug("Metadata received for RTSP session", "sessionId", s.ID(), "streamId", streamId)
	return nil
}

// convertFrameToRTP media.Frame을 RTP 패킷으로 변환
func (s *Session) convertFrameToRTP(frame media.Frame) ([]byte, error) {
	// 간단한 RTP 패킷 생성 (실제 구현에서는 더 정교한 변환 필요)
	// RTP 헤더 (12바이트) + 페이로드
	
	// 프레임 데이터를 평면 배열로 결합
	var payload []byte
	for _, chunk := range frame.Data {
		payload = append(payload, chunk...)
	}

	// 간단한 RTP 헤더 생성
	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80 // V=2, P=0, X=0, CC=0
	
	// 프레임 타입에 따라 페이로드 타입 설정
	if frame.Type == media.TypeVideo {
		rtpHeader[1] = 96 // H.264 페이로드 타입
	} else if frame.Type == media.TypeAudio {
		rtpHeader[1] = 97 // AAC 페이로드 타입
	}
	
	// 타임스탬프 설정 (간단히 frame.Timestamp 사용)
	timestamp := frame.Timestamp
	rtpHeader[4] = byte(timestamp >> 24)
	rtpHeader[5] = byte(timestamp >> 16)
	rtpHeader[6] = byte(timestamp >> 8)
	rtpHeader[7] = byte(timestamp)
	
	// SSRC 설정 (세션 포인터 주소 사용)
	ssrc := uint32(uintptr(unsafe.Pointer(s)))
	rtpHeader[8] = byte(ssrc >> 24)
	rtpHeader[9] = byte(ssrc >> 16)
	rtpHeader[10] = byte(ssrc >> 8)
	rtpHeader[11] = byte(ssrc)

	// RTP 패킷 = 헤더 + 페이로드
	rtpPacket := append(rtpHeader, payload...)
	
	return rtpPacket, nil
}

// GetStreamPath 현재 세션의 스트림 경로 반환
func (s *Session) GetStreamPath() string {
	return s.streamPath
}

// MediaSource 인터페이스 구현 (RECORD 시 사용)
// RTSP Session은 RECORD 모드에서는 MediaSource로, PLAY 모드에서는 MediaSink로 동작

// convertRTPToFrame RTP 패킷을 media.Frame으로 변환 (RECORD 시 사용)
func (s *Session) convertRTPToFrame(rtpData []byte, streamId string) (media.Frame, error) {
	if len(rtpData) < 12 {
		return media.Frame{}, fmt.Errorf("RTP packet too short: %d bytes", len(rtpData))
	}

	// RTP 헤더 파싱
	version := (rtpData[0] & 0xC0) >> 6
	if version != 2 {
		return media.Frame{}, fmt.Errorf("unsupported RTP version: %d", version)
	}

	payloadType := rtpData[1] & 0x7F
	timestamp := uint32(rtpData[4])<<24 | uint32(rtpData[5])<<16 | uint32(rtpData[6])<<8 | uint32(rtpData[7])

	// 페이로드 추출 (헤더 제외)
	payload := rtpData[12:]

	// 페이로드 타입에 따른 미디어 타입 결정
	var mediaType media.Type

	switch payloadType {
	case 96: // H.264
		mediaType = media.TypeVideo
	case 97: // AAC
		mediaType = media.TypeAudio
	default:
		mediaType = media.TypeVideo
	}

	// media.Frame 생성
	var subType media.FrameSubType
	if mediaType == media.TypeVideo {
		subType = media.VideoInterFrame // 기본값으로 Inter Frame
	} else {
		subType = media.AudioRawData    // 기본값으로 Raw Data
	}
	
	frame := media.Frame{
		Type:      mediaType,
		SubType:   subType,
		Timestamp: timestamp,
		Data:      [][]byte{payload}, // 단일 청크로 저장
	}

	slog.Debug("Converted RTP to Frame", 
		"sessionId", s.ID(), 
		"streamId", streamId,
		"payloadType", payloadType,
		"mediaType", mediaType,
		"timestamp", timestamp,
		"payloadSize", len(payload))

	return frame, nil
}

// --- MediaSource 인터페이스 구현 (RECORD 모드) ---

// PublishingStreams MediaSource 인터페이스 구현 - 발행 중인 스트림 목록 반환 (RECORD 모드)
func (s *Session) PublishingStreams() []*media.Stream {
	if s.state == StateRecording && s.Stream != nil {
		return []*media.Stream{s.Stream}
	}
	return nil
}

// --- MediaSink 인터페이스 구현 (PLAY 모드) ---

// SubscribedStreams MediaSink 인터페이스 구현 - 구독 중인 스트림 ID 목록 반환 (PLAY 모드)
func (s *Session) SubscribedStreams() []string {
	if s.state == StatePlaying && s.Stream != nil {
		return []string{s.Stream.GetId()}
	}
	return nil
}
