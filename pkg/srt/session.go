package srt

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sol/pkg/media"
	"sync"
	"time"
)

// Session SRT 세션 구조체
type Session struct {
	socketID   uint32
	remoteAddr *net.UDPAddr
	config     SRTConfig
	server     *Server

	// 상태
	state        int
	ctx          context.Context
	cancel       context.CancelFunc
	lastActivity time.Time
	connectedAt  time.Time

	// MediaSource/MediaSink 인터페이스
	mediaSource media.MediaSource
	mediaSink   media.MediaSink
	streamID    string
	isPublisher bool
	isPlayer    bool

	// 패킷 처리
	sequenceNumber uint32
	packetBuffer   map[uint32]*UDTPacket
	bufferMutex    sync.RWMutex

	// 암호화
	encryption *SRTEncryption

	// 신뢰성 관리
	reliability *SRTReliability

	// 통계
	stats      ConnectionInfo
	statsMutex sync.RWMutex

	// 채널
	mediaServerChannel chan<- any
	dataChannel        chan []byte
	controlChannel     chan *UDTPacket

	// 동기화
	wg sync.WaitGroup
}

// NewSession 새 SRT 세션 생성
func NewSession(socketID uint32, remoteAddr *net.UDPAddr, config SRTConfig,
	mediaServerChannel chan<- any, server *Server) *Session {

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()

	// 암호화 초기화
	encType := ParseEncryptionType(config.GetEncryptionString())
	encryption, err := NewSRTEncryption(encType, config.Passphrase)
	if err != nil {
		slog.Error("Failed to initialize SRT encryption", "err", err)
		encryption, _ = NewSRTEncryption(EncryptionNone, "")
	}

	session := &Session{
		socketID:           socketID,
		remoteAddr:         remoteAddr,
		config:             config,
		server:             server,
		state:              StateInit,
		ctx:                ctx,
		cancel:             cancel,
		lastActivity:       now,
		connectedAt:        now,
		packetBuffer:       make(map[uint32]*UDTPacket),
		encryption:         encryption,
		mediaServerChannel: mediaServerChannel,
		dataChannel:        make(chan []byte, 1000),
		controlChannel:     make(chan *UDTPacket, 100),
		stats: ConnectionInfo{
			RemoteAddr:   remoteAddr,
			SocketID:     socketID,
			ConnectedAt:  now,
			LastActivity: now,
		},
	}

	// 신뢰성 관리 초기화
	reliabilityConfig := ReliabilityConfig{
		MaxRetries:        MaxRetries,
		RetryInterval:     time.Duration(RetryInterval) * time.Millisecond,
		AckTimeout:        1000 * time.Millisecond,
		LossDetectionTime: 500 * time.Millisecond,
		BufferSize:        1000,
	}
	session.reliability = NewSRTReliability(session, reliabilityConfig)

	// 세션 처리 고루틴 시작
	go session.eventLoop()
	go session.dataProcessor()

	return session
}

// Close 세션 종료
func (s *Session) Close() {
	if s.state == StateClosed {
		return
	}

	slog.Info("Closing SRT session", "socketID", s.socketID)

	s.state = StateClosing

	// MediaSource/MediaSink 정리
	s.cleanupMedia()

	// 컨텍스트 취소
	s.cancel()

	// 채널 닫기
	close(s.dataChannel)
	close(s.controlChannel)

	// 고루틴 대기
	s.wg.Wait()

	s.state = StateClosed

	slog.Info("SRT session closed", "socketID", s.socketID)
}

// HandlePacket 패킷 처리
func (s *Session) HandlePacket(data []byte) {
	s.updateActivity()

	s.statsMutex.Lock()
	s.stats.PacketsReceived++
	s.stats.BytesReceived += uint64(len(data))
	s.statsMutex.Unlock()

	// 패킷 타입 확인
	if s.isControlPacket(data) {
		s.handleControlPacket(data)
	} else {
		s.handleDataPacket(data)
	}
}

// isControlPacket 컨트롤 패킷 여부 확인
func (s *Session) isControlPacket(data []byte) bool {
	if len(data) < 1 {
		return false
	}
	return (data[0] & 0x80) != 0
}

// handleControlPacket 컨트롤 패킷 처리
func (s *Session) handleControlPacket(data []byte) {
	packet, err := ParseUDTPacket(data)
	if err != nil {
		slog.Error("Failed to parse UDT packet", "err", err)
		return
	}

	if !packet.Header.IsControlPacket() {
		slog.Debug("Not a control packet")
		return
	}

	controlType := packet.Header.GetControlType()
	slog.Debug("Processing control packet", "type", controlType, "socketID", s.socketID)

	switch controlType {
	case ControlHandshake:
		s.handleHandshake(packet)
	case ControlKeepAlive:
		s.handleKeepAlive(packet)
	case ControlAck:
		s.handleAck(packet)
	case ControlNak:
		s.handleNak(packet)
	case ControlShutdown:
		s.handleShutdown(packet)
	default:
		slog.Debug("Unknown control packet type", "type", controlType)
	}
}

// handleDataPacket 데이터 패킷 처리
func (s *Session) handleDataPacket(data []byte) {
	packet, err := ParseUDTPacket(data)
	if err != nil {
		slog.Error("Failed to parse data packet", "err", err)
		return
	}

	if packet.Header.IsControlPacket() {
		slog.Debug("Received control packet in data handler")
		return
	}

	seqNum := packet.Header.GetSequenceNumber()
	msgNum := packet.Header.MessageNumber

	slog.Debug("Received data packet",
		"socketID", s.socketID,
		"seqNum", seqNum,
		"msgNum", msgNum,
		"payloadSize", len(packet.Payload),
		"encrypted", s.encryption.IsEnabled())

	// 시퀀스 번호 검증 및 순서 보장 (향후 구현)
	// 현재는 간단히 페이로드만 처리

	if len(packet.Payload) > 0 {
		// 암호화된 경우 복호화
		if s.encryption.IsEnabled() {
			if err := s.encryption.DecryptPacket(packet); err != nil {
				slog.Error("Failed to decrypt packet", "err", err, "socketID", s.socketID)
				return
			}
			slog.Debug("Packet decrypted successfully", "socketID", s.socketID, "originalSize", len(data), "decryptedSize", len(packet.Payload))
		}

		// 신뢰성 관리를 통한 패킷 수신 처리
		if err := s.reliability.ReceivePacket(seqNum, packet.Payload); err != nil {
			slog.Error("Failed to process packet with reliability", "err", err, "socketID", s.socketID)
			return
		}
	}

	// 신뢰성 관리에서 ACK 처리 (자동으로 스케줄링됨)

	s.updateActivity()
}

// sendAck ACK 패킷 전송
func (s *Session) sendAck(ackSeqNum uint32) {
	ackPacket := CreateAckPacket(s.socketID, ackSeqNum)
	if err := s.server.SendToClient(s.remoteAddr, ackPacket); err != nil {
		slog.Error("Failed to send ACK", "err", err, "ackSeqNum", ackSeqNum)
	}
}

// sendDataPacket 데이터 패킷 전송
func (s *Session) sendDataPacket(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if s.state != StateConnected {
		return fmt.Errorf("session not connected")
	}

	// 시퀀스 번호 증가
	s.sequenceNumber++

	// 메시지 번호 (단순히 시퀀스 번호와 동일하게 설정)
	messageNum := s.sequenceNumber

	// 데이터 패킷 생성
	packet, err := ParseUDTPacket(CreateDataPacket(s.sequenceNumber, messageNum, s.stats.PeerSocketID, data))
	if err != nil {
		return fmt.Errorf("failed to create data packet: %w", err)
	}

	// 암호화가 활성화된 경우 패킷 암호화
	if s.encryption.IsEnabled() {
		if err := s.encryption.EncryptPacket(packet); err != nil {
			return fmt.Errorf("failed to encrypt packet: %w", err)
		}
		slog.Debug("Packet encrypted for transmission", 
			"socketID", s.socketID, 
			"originalSize", len(data), 
			"encryptedSize", len(packet.Payload))
	}

	// 패킷 직렬화
	serializedPacket := append(SerializeUDTHeader(&packet.Header), packet.Payload...)

	// 통계 업데이트
	s.statsMutex.Lock()
	s.stats.PacketsSent++
	s.stats.BytesSent += uint64(len(serializedPacket))
	s.statsMutex.Unlock()

	return s.server.SendToClient(s.remoteAddr, serializedPacket)
}

// handleHandshake SRT 핸드셰이크 처리
func (s *Session) handleHandshake(packet *UDTPacket) {
	slog.Debug("Processing SRT handshake", "socketID", s.socketID, "state", s.state)

	// 핸드셰이크 패킷 파싱
	handshakeReq, err := ParseSRTHandshake(append(SerializeUDTHeader(&packet.Header), packet.Payload...))
	if err != nil {
		slog.Error("Failed to parse SRT handshake", "err", err)
		s.sendHandshakeRejection("Invalid handshake format")
		return
	}

	// SRT 버전 체크
	if handshakeReq.UDTVersion != 4 {
		slog.Error("Unsupported UDT version", "version", handshakeReq.UDTVersion)
		s.sendHandshakeRejection("Unsupported UDT version")
		return
	}

	// 소켓 타입 체크 (1 = 스트림)
	if handshakeReq.SocketType != 1 {
		slog.Error("Unsupported socket type", "type", handshakeReq.SocketType)
		s.sendHandshakeRejection("Unsupported socket type")
		return
	}

	// 핸드셰이크 타입에 따른 처리
	switch handshakeReq.HandshakeType {
	case 1: // 연결 요청
		s.handleHandshakeRequest(handshakeReq)
	case 0: // 연결 응답
		s.handleHandshakeResponse(handshakeReq)
	default:
		slog.Error("Unknown handshake type", "type", handshakeReq.HandshakeType)
		s.sendHandshakeRejection("Unknown handshake type")
	}
}

// handleHandshakeRequest 연결 요청 처리
func (s *Session) handleHandshakeRequest(req *SRTHandshakePacket) {
	slog.Info("Processing SRT connection request",
		"clientSocketID", req.SocketID,
		"serverSocketID", s.socketID,
		"maxPacketSize", req.MaxPacketSize)

	// 클라이언트 소켓 ID 저장
	s.stats.PeerSocketID = req.SocketID

	// StreamID 추출 (SRT 확장에서)
	s.extractStreamID(req)

	// 핸드셰이크 응답 생성 및 전송
	latency := uint16(s.config.Latency)
	response := CreateSRTHandshakeResponse(req, s.socketID, latency)

	if err := s.server.SendToClient(s.remoteAddr, response); err != nil {
		slog.Error("Failed to send handshake response", "err", err)
		return
	}

	// 상태를 연결됨으로 변경
	s.state = StateConnected
	s.stats.State = StateConnected

	// 연결 이벤트 전송
	event := SRTConnectionEvent{
		SocketID:   s.socketID,
		RemoteAddr: s.remoteAddr,
		StreamID:   s.streamID,
	}

	select {
	case s.mediaServerChannel <- event:
		slog.Info("SRT connection established",
			"socketID", s.socketID,
			"peerSocketID", s.stats.PeerSocketID,
			"streamID", s.streamID)
	default:
		slog.Debug("Media server channel full")
	}

	// 스트림 모드 결정 (발행자/재생자)
	s.determineStreamMode()
}

// handleHandshakeResponse 연결 응답 처리 (클라이언트 모드)
func (s *Session) handleHandshakeResponse(resp *SRTHandshakePacket) {
	slog.Debug("Received handshake response", "serverSocketID", resp.SocketID)

	// 서버가 우리 요청을 수락했음을 확인
	s.stats.PeerSocketID = resp.SocketID
	s.state = StateConnected
	s.stats.State = StateConnected

	slog.Info("SRT connection confirmed",
		"clientSocketID", s.socketID,
		"serverSocketID", resp.SocketID)
}

// sendHandshakeRejection 핸드셰이크 거절 전송
func (s *Session) sendHandshakeRejection(reason string) {
	slog.Info("Rejecting handshake", "reason", reason, "socketID", s.socketID)

	// 거절 패킷 생성 (핸드셰이크 타입 -1로 설정)
	rejectionPacket := CreateControlPacket(ControlHandshake, s.socketID, 0xFFFFFFFF)

	if err := s.server.SendToClient(s.remoteAddr, rejectionPacket); err != nil {
		slog.Error("Failed to send handshake rejection", "err", err)
	}

	// 세션 종료
	s.Close()
}

// extractStreamID SRT StreamID 추출
func (s *Session) extractStreamID(req *SRTHandshakePacket) {
	// 핸드셰이크 확장에서 StreamID 추출 시도
	rawData := append(SerializeUDTHeader(&UDTHeader{
		SeqNum:        uint32(ControlHandshake) | 0x80000000,
		MessageNumber: 0,
		Timestamp:     req.SRTFlags, // 임시로 SRTFlags 사용
		DestSocketID:  req.SocketID,
	}), make([]byte, 56)...) // 기본 핸드셰이크 크기만큼 더미 데이터

	streamID, err := ExtractStreamIDFromHandshake(req, rawData)
	if err != nil {
		slog.Debug("Failed to extract StreamID from handshake", "err", err)
		// 기본 StreamID 생성
		streamID = fmt.Sprintf("srt_%d", req.SocketID)
	}

	// StreamID 검증
	if err := ValidateStreamID(streamID); err != nil {
		slog.Warn("Invalid StreamID, using default", "streamID", streamID, "err", err)
		streamID = fmt.Sprintf("srt_%d", req.SocketID)
	}

	s.streamID = streamID
	s.stats.StreamID = s.streamID

	slog.Info("Stream ID extracted", "streamID", s.streamID)
}

// determineStreamMode 스트림 모드 결정 (발행자/재생자)
func (s *Session) determineStreamMode() {
	// StreamID에서 모드 파싱
	streamInfo, err := ParseStreamID(s.streamID)
	if err != nil {
		slog.Debug("Failed to parse StreamID for mode detection", "err", err)
		// 기본적으로 발행자로 설정
		s.isPublisher = true
		s.isPlayer = false
	} else {
		// StreamID 기반 모드 설정
		s.isPublisher = streamInfo.IsPublishMode()
		s.isPlayer = streamInfo.IsPlayMode()

		// 둘 다 아닌 경우 기본값 설정
		if !s.isPublisher && !s.isPlayer {
			s.isPublisher = true // 기본적으로 발행자
		}

		slog.Info("Stream mode determined from StreamID",
			"streamID", s.streamID,
			"mode", streamInfo.Mode,
			"resource", streamInfo.GetStreamKey(),
			"isPublisher", s.isPublisher,
			"isPlayer", s.isPlayer)

		// 스트림 키 업데이트
		streamKey := streamInfo.GetStreamKey()
		if streamKey != "" {
			s.streamID = streamKey
			s.stats.StreamID = s.streamID
		}
	}

	// 서버의 스트림 세션 맵에 추가
	s.server.AddStreamSession(s.streamID, s)

	// MediaSource/MediaSink 초기화
	if s.isPublisher {
		s.initializeMediaSource()
	}
	if s.isPlayer {
		s.initializeMediaSink()
	}
}

// handleKeepAlive 킵얼라이브 처리
func (s *Session) handleKeepAlive(packet *UDTPacket) {
	slog.Debug("Received keep-alive", "socketID", s.socketID)

	// 킵얼라이브 응답 전송
	response := CreateKeepAlivePacket(s.socketID)
	if err := s.server.SendToClient(s.remoteAddr, response); err != nil {
		slog.Error("Failed to send keep-alive response", "err", err)
	}

	// 활동 시간 업데이트
	s.updateActivity()
}

// handleAck ACK 처리
func (s *Session) handleAck(packet *UDTPacket) {
	ackSeqNum := packet.Header.MessageNumber
	slog.Debug("Received ACK", "socketID", s.socketID, "ackSeqNum", ackSeqNum)

	// 신뢰성 관리자를 통한 ACK 처리
	s.reliability.ProcessAck(ackSeqNum)
	s.updateActivity()
}

// handleNak NAK 처리 (재전송 요청)
func (s *Session) handleNak(packet *UDTPacket) {
	lostSeqNum := packet.Header.MessageNumber
	slog.Debug("Received NAK", "socketID", s.socketID, "lostSeqNum", lostSeqNum)

	// 신뢰성 관리자를 통한 NAK 처리
	s.reliability.ProcessNak(lostSeqNum)
	s.updateActivity()
}

// handleShutdown 종료 처리
func (s *Session) handleShutdown(packet *UDTPacket) {
	slog.Info("Received shutdown request", "socketID", s.socketID)

	// 종료 응답 전송
	shutdownResponse := CreateControlPacket(ControlShutdown, s.socketID, 0)
	if err := s.server.SendToClient(s.remoteAddr, shutdownResponse); err != nil {
		slog.Error("Failed to send shutdown response", "err", err)
	}

	// 세션 종료
	s.Close()
}

// sendControlPacket 컨트롤 패킷 전송
func (s *Session) sendControlPacket(data []byte) error {
	if s.server == nil {
		return fmt.Errorf("server not available")
	}

	s.statsMutex.Lock()
	s.stats.PacketsSent++
	s.stats.BytesSent += uint64(len(data))
	s.statsMutex.Unlock()

	return s.server.SendToClient(s.remoteAddr, data)
}

// eventLoop 이벤트 처리 루프
func (s *Session) eventLoop() {
	s.wg.Add(1)
	defer s.wg.Done()

	keepAliveTicker := time.NewTicker(1 * time.Second)
	defer keepAliveTicker.Stop()

	reliabilityTicker := time.NewTicker(100 * time.Millisecond) // 더 자주 처리
	defer reliabilityTicker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case packet := <-s.controlChannel:
			s.processControlPacket(packet)
		case <-keepAliveTicker.C:
			s.sendKeepAlive()
		case <-reliabilityTicker.C:
			// ACK/NAK/재전송 큐 처리
			s.reliability.ProcessQueues()
		}
	}
}

// dataProcessor 데이터 처리 고루틴
func (s *Session) dataProcessor() {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case data := <-s.dataChannel:
			s.processMediaData(data)
		}
	}
}

// processControlPacket 컨트롤 패킷 처리
func (s *Session) processControlPacket(packet *UDTPacket) {
	// 컨트롤 패킷 처리 로직 (현재 handleControlPacket에서 처리)
}

// processMediaData 미디어 데이터 처리
func (s *Session) processMediaData(data []byte) {
	if len(data) == 0 {
		return
	}

	// SRT StreamID 파싱 및 발행/재생 모드 결정
	s.parseStreamMode(data)

	if s.isPublisher {
		s.handlePublishData(data)
	} else if s.isPlayer {
		s.handlePlayData(data)
	}
}

// parseStreamMode 스트림 모드 파싱
func (s *Session) parseStreamMode(data []byte) {
	if s.streamID != "" {
		return // 이미 파싱됨
	}

	// 간단한 StreamID 파싱 (실제로는 더 복잡한 로직 필요)
	// 예: "publish:stream1" 또는 "play:stream1"
	if len(data) > 10 {
		s.streamID = fmt.Sprintf("srt_stream_%d", s.socketID)
		s.isPublisher = true // 기본적으로 발행자로 설정
	}
}

// handlePublishData 발행 데이터 처리
func (s *Session) handlePublishData(data []byte) {
	if !s.isPublisher {
		slog.Warn("Received publish data from non-publisher session", "socketID", s.socketID)
		return
	}

	if s.mediaSource == nil {
		s.initializeMediaSource()
	}

	slog.Debug("SRT publish data received", "streamID", s.streamID, "dataSize", len(data))

	// 같은 스트림의 모든 재생자에게 데이터 브로드캐스트
	s.server.BroadcastToStreamPlayers(s.streamID, data)

	// MediaServer로 프레임 데이터 전송 (다른 프로토콜과의 연동을 위해)
	if len(data) > 0 {
		// 간단한 구현: 원시 데이터를 VideoFrame으로 래핑
		frame := media.Frame{
			Type:      media.TypeVideo,
			SubType:   media.VideoKeyFrame,                     // 임시로 키프레임으로 설정
			Timestamp: uint32(time.Now().UnixNano() / 1000000), // ms
			Data:      [][]byte{data},
		}

		// MediaSource를 통해 프레임 전송 (향후 구현)
		_ = frame // 현재는 사용하지 않음
	}
}

// handlePlayData 재생 데이터 처리
func (s *Session) handlePlayData(data []byte) {
	if !s.isPlayer {
		slog.Warn("Received play request from non-player session", "socketID", s.socketID)
		return
	}

	if s.mediaSink == nil {
		s.initializeMediaSink()
	}

	slog.Debug("SRT play request received", "streamID", s.streamID)

	// 해당 스트림에 발행자가 있는지 확인
	publisher := s.server.GetStreamPublisher(s.streamID)
	if publisher == nil {
		slog.Debug("No publisher found for stream", "streamID", s.streamID)
		// 발행자가 나타날 때까지 대기 (실제로는 타임아웃 처리 필요)
		return
	}

	slog.Info("Player connected to stream",
		"streamID", s.streamID,
		"playerSocketID", s.socketID,
		"publisherSocketID", publisher.socketID)
}

// initializeMediaSource MediaSource 초기화
func (s *Session) initializeMediaSource() {
	s.mediaSource = &srtMediaSource{session: s}

	// MediaServer에 노드 생성 이벤트 전송
	nodeEvent := media.NewNodeCreated(uintptr(s.socketID), media.NodeTypeSRT, s.mediaSource)

	select {
	case s.mediaServerChannel <- nodeEvent:
		slog.Info("SRT MediaSource node created", "streamID", s.streamID, "socketID", s.socketID)
	default:
		slog.Error("Failed to send node created event")
	}

}

// initializeMediaSink MediaSink 초기화
func (s *Session) initializeMediaSink() {
	s.mediaSink = &srtMediaSink{session: s}

	// MediaServer에 노드 생성 이벤트 전송
	nodeEvent := media.NewNodeCreated(uintptr(s.socketID), media.NodeTypeSRT, s.mediaSink)

	select {
	case s.mediaServerChannel <- nodeEvent:
		slog.Info("SRT MediaSink node created", "streamID", s.streamID, "socketID", s.socketID)
	default:
		slog.Error("Failed to send node created event")
	}

}

// sendKeepAlive 킵얼라이브 전송
func (s *Session) sendKeepAlive() {
	if s.state != StateConnected {
		return
	}

	keepAlive := CreateKeepAlivePacket(s.socketID)
	if err := s.server.SendToClient(s.remoteAddr, keepAlive); err != nil {
		slog.Error("Failed to send keep-alive", "err", err)
	}
}

// cleanupMedia 미디어 리소스 정리
func (s *Session) cleanupMedia() {
	// 신뢰성 관리자 정리
	if s.reliability != nil {
		s.reliability.Close()
	}

	// 서버의 스트림 세션 맵에서 제거
	if s.streamID != "" {
		s.server.RemoveStreamSession(s.streamID, s)
	}

	if s.isPublisher && s.streamID != "" {
		event := media.NewPublishStopped(uintptr(s.socketID), media.NodeTypeSRT, s.streamID)
		select {
		case s.mediaServerChannel <- event:
		default:
		}
	}

	if s.isPlayer && s.streamID != "" {
		event := media.NewPlayStopped(uintptr(s.socketID), media.NodeTypeSRT, s.streamID)
		select {
		case s.mediaServerChannel <- event:
		default:
		}
	}

	// 노드 종료 이벤트 전송
	terminateEvent := media.NewNodeTerminated(uintptr(s.socketID), media.NodeTypeSRT)
	select {
	case s.mediaServerChannel <- terminateEvent:
	default:
	}
}

// updateActivity 활동 시간 업데이트
func (s *Session) updateActivity() {
	s.statsMutex.Lock()
	s.lastActivity = time.Now()
	s.stats.LastActivity = s.lastActivity
	s.statsMutex.Unlock()
}

// LastActivity 마지막 활동 시간 반환
func (s *Session) LastActivity() time.Time {
	s.statsMutex.RLock()
	defer s.statsMutex.RUnlock()
	return s.lastActivity
}

// GetStats 세션 통계 반환
func (s *Session) GetStats() ConnectionInfo {
	s.statsMutex.RLock()
	defer s.statsMutex.RUnlock()
	return s.stats
}

// GetStreamID 스트림 ID 반환
func (s *Session) GetStreamID() string {
	return s.streamID
}

// IsPublisher 발행자 여부 반환
func (s *Session) IsPublisher() bool {
	return s.isPublisher
}

// IsPlayer 재생자 여부 반환
func (s *Session) IsPlayer() bool {
	return s.isPlayer
}
