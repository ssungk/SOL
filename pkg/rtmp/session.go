package rtmp

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sol/pkg/media"
	"sol/pkg/rtmp/amf"
	"sol/pkg/utils"
	"sync"
	"unsafe"
)

// session RTMP에서 사용하는 세션 구조체 (이벤트 루프 기반)
type session struct {
	reader        *messageReader
	writer        *messageWriter
	conn          net.Conn
	bufReadWriter *bufio.ReadWriter

	// 스트림 관리 (이벤트 루프 내에서만 접근)
	appName string

	// 발행용 (MediaSource) - RTMP streamID 기반
	publishedStreams map[uint32]*media.Stream // streamID(uint32) -> Stream

	// 상호 참조 테이블
	streamIDToPath map[uint32]string // streamID -> streamPath
	streamPathToId map[string]uint32 // streamPath -> streamID
	nextStreamId   int               // 다음 할당할 streamID (1부터 시작)

	// 재생용 (MediaSink)
	subscribedStreams map[uint32]string // streamID -> streamPath 구독 스트림 매핑

	// MediaServer와의 통신 관리
	mediaServerChannel chan<- any
	wg                 *sync.WaitGroup

	// 동시성 및 생명주기 관리
	channel chan any
	ctx     context.Context
	cancel  context.CancelFunc
}

// newSession 새로운 세션 생성 (내부 사용)
func newSession(conn net.Conn, mediaServerChannel chan<- any, wg *sync.WaitGroup) *session {
	ctx, cancel := context.WithCancel(context.Background())
	
	// bufio.ReadWriter 생성 (8KB 버퍼 크기)
	reader := bufio.NewReaderSize(conn, 8192)
	writer := bufio.NewWriterSize(conn, 8192)
	bufReadWriter := bufio.NewReadWriter(reader, writer)
	
	s := &session{
		reader:             newMessageReader(),
		writer:             newMessageWriter(),
		conn:               conn,
		bufReadWriter:      bufReadWriter,
		mediaServerChannel: mediaServerChannel,
		channel:            make(chan any, media.DefaultChannelBufferSize),
		ctx:                ctx,
		cancel:             cancel,
		wg:                 wg,
		publishedStreams:   make(map[uint32]*media.Stream), // 발행용 스트림 맵 초기화
		streamIDToPath:     make(map[uint32]string),        // streamID -> streamPath 매핑
		streamPathToId:     make(map[string]uint32),        // streamPath -> streamID 매핑
		nextStreamId:       1,                              // 1부터 시작
		subscribedStreams:  make(map[uint32]string),        // 재생용 스트림 매핑 초기화
	}

	// NodeCreated 이벤트를 MediaServer로 전송
	s.mediaServerChannel <- media.NewNodeCreated(s.ID(), s)

	// 세션의 두 주요 고루틴 시작
	go s.eventLoop()
	go s.readLoop()

	return s
}

// eventLoop 세션의 모든 상태 변경과 I/O 쓰기를 처리하는 메인 루프
func (s *session) eventLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer s.cleanup()

	for {
		select {
		case data := <-s.channel:
			s.handleChannel(data)
		case <-s.ctx.Done():
			return
		}
	}
}

// handleMessageOrRoute 메시지를 즉시 처리하거나 이벤트 루프로 라우팅 (레이스 컨디션 방지)
func (s *session) handleMessageOrRoute(message *Message) {
	switch message.messageHeader.typeId {
	case MsgTypeSetChunkSize:
		s.handleSetChunkSize(message)
	case MsgTypeAbort:
		s.handleAbort(message)
	default:
		select {
		case s.channel <- commandEvent{message: message}:
		case <-s.ctx.Done():

		}
	}
}

// readLoop 클라이언트로부터 들어오는 RTMP 메시지를 읽는 루프
func (s *session) readLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer s.cancel()

	if err := ServerHandshake(s.bufReadWriter); err != nil {
		slog.Error("Handshake failed", "err", err)
		return
	}

	for {
		message, err := s.reader.readNextMessage(s.bufReadWriter)
		if err != nil {
			return
		}

		// 메시지를 즉시 처리하거나 이벤트 루프로 라우팅
		s.handleMessageOrRoute(message)
	}
}

// handleChannel 이벤트 종류에 따라 적절한 핸들러 호출
func (s *session) handleChannel(data any) {
	switch e := data.(type) {
	case sendPacketEvent:
		s.handleSendPacket(e)
	case sendMetadataEvent:
		s.handleSendMetadata(e)
	case commandEvent:
		s.handleCommand(e.message)
	default:
		slog.Error("Unknown session data type", "type", utils.TypeName(data))
	}
}

// cleanup 세션 종료 시 모든 리소스를 정리
func (s *session) cleanup() {
	utils.CloseWithLog(s.conn)

	// 발행자 또는 플레이어였다면, MediaServer에 종료 이벤트를 보냄
	s.stopPublishing()
	s.stopSubscribing()

	// MediaServer에 최종 종료 알림
	s.mediaServerChannel <- media.NewNodeTerminated(s.ID())

}

// --- MediaSink 인터페이스 구현 ---

// sendChannelEvent 채널에 이벤트를 안전하게 전송하는 공통 헬퍼 함수
func (s *session) sendChannelEvent(event any, eventType string) error {
	select {
	case s.channel <- event:
		return nil
	case <-s.ctx.Done():
		return fmt.Errorf("session closed, cannot send %s: %w", eventType, s.ctx.Err())
	}
}

func (s *session) SendPacket(streamID string, packet media.Packet) error {
	// RTMP는 현재 비디오(트랙0), 오디오(트랙1)만 지원
	if packet.TrackIndex > 1 {
		return nil // 추가 트랙은 무시
	}

	event := sendPacketEvent{streamPath: streamID, packet: packet}
	return s.sendChannelEvent(event, "packet")
}

func (s *session) SendMetadata(streamID string, metadata map[string]string) error {
	event := sendMetadataEvent{streamPath: streamID, metadata: metadata}
	return s.sendChannelEvent(event, "metadata")
}

// --- 이벤트 핸들러 ---

// handleSendPacket MediaSink로부터 받은 패킷을 클라이언트로 전송
func (s *session) handleSendPacket(e sendPacketEvent) {
	streamID, exists := s.streamPathToId[e.streamPath]
	if !exists {
		slog.Warn("Stream mapping not found, skipping packet", "sessionId", s.ID(), "streamPath", e.streamPath)
		return
	}

	var msgType uint8
	var header []byte

	if e.packet.IsVideo() {
		msgType = MsgTypeVideo
		// 비디오: RTMP 헤더 재생성 (Packet의 CTS 사용)
		header = GenerateVideoHeader(e.packet, e.packet.CTS)

	} else if e.packet.IsAudio() {
		msgType = MsgTypeAudio
		// 오디오: RTMP 헤더 재생성
		header = GenerateAudioHeader(e.packet)

	} else {
		slog.Warn("Unsupported packet type", "codec", e.packet.Codec, "sessionId", s.ID())
		return
	}

	// 순수 데이터만 전송 (미디어 헤더는 별도 저장)
	var totalLen int
	for _, buffer := range e.packet.Data {
		totalLen += len(buffer.Data())
	}
	
	// Message Header 길이는 실제 페이로드 길이 (미디어헤더 + 데이터)
	payloadLen := len(header) + totalLen
	messageHeader := newMessageHeader(e.packet.DTS32(), uint32(payloadLen), msgType, uint32(streamID))
	message := NewMessage(&messageHeader)
	
	// 미디어 헤더 저장
	message.mediaHeader = make([]byte, len(header))
	copy(message.mediaHeader, header)
	
	// 순수 데이터 버퍼들을 직접 참조 (zero-copy)
	if len(e.packet.Data) > 0 {
		message.payloads = make([]*media.Buffer, len(e.packet.Data))
		for i, buffer := range e.packet.Data {
			message.payloads[i] = buffer.AddRef() // 참조 카운트 증가
		}
	}
	if err := s.writer.writeMessage(s.bufReadWriter, message); err != nil {
		slog.Error("Failed to send packet to RTMP session", "sessionId", s.ID(), "err", err)
		message.Release() // 오류 시에도 메모리 해제
		return
	}
	// 즉시 플러시하여 전송 보장
	s.bufReadWriter.Flush()
	message.Release() // 전송 완료 후 메모리 해제
}

// handleSendMetadata MediaSink로부터 받은 메타데이터를 클라이언트로 전송
func (s *session) handleSendMetadata(e sendMetadataEvent) {
	streamID, exists := s.streamPathToId[e.streamPath]
	if !exists {
		slog.Warn("Stream mapping not found, skipping metadata", "sessionId", s.ID(), "streamPath", e.streamPath)
		return
	}

	if err := s.sendMetadataToClient(e.metadata, uint32(streamID)); err != nil {
		slog.Error("Failed to send metadata to RTMP session", "sessionId", s.ID(), "err", err)
	}
}

// handleCommand 클라이언트로부터 받은 RTMP 메시지 처리
func (s *session) handleCommand(message *Message) {
	switch message.messageHeader.typeId {
	case MsgTypeSetChunkSize:
		// SetChunkSize는 readLoop에서 직접 처리됨 (레이스 컨디션 방지)
		// 이 케이스는 도달하지 않음
	case MsgTypeAbort:
		// Abort는 readLoop에서 직접 처리됨 (레이스 컨디션 방지)
		// 이 케이스는 도달하지 않음
	case MsgTypeAcknowledgement:
	case MsgTypeUserControl:
	case MsgTypeWindowAckSize:
	case MsgTypeSetPeerBW:
	case MsgTypeAudio:
		s.handleAudio(message)
	case MsgTypeVideo:
		s.handleVideo(message)
	case MsgTypeAMF0Data:
		s.handleAMF0ScriptData(message)
	case MsgTypeAMF3Data:
		s.handleAMF3ScriptData(message)
	case MsgTypeAMF0Command:
		s.handleAMF0Command(message)
	case MsgTypeAMF3Command:
		s.handleAMF3Command(message)
	default:
		slog.Warn("unhandled RTMP message type", "sessionId", s.ID(), "type", message.messageHeader.typeId)
	}
	
	// 메시지 처리 완료 후 메모리 해제
	message.Release()
}

// --- MediaNode 인터페이스 및 기타 헬퍼 ---

func (s *session) ID() uintptr {
	return uintptr(unsafe.Pointer(s))
}

func (s *session) NodeType() media.NodeType {
	return media.NodeTypeRTMP
}

func (s *session) Address() string {
	return s.conn.RemoteAddr().String()
}

func (s *session) Close() error {
	slog.Info("RTMP session stopping", "sessionId", s.ID())
	s.cancel()
	return nil
}

func (s *session) IsPublisher() bool {
	return s.isPublishingMode()
}

// 오디오 데이터 처리
func (s *session) handleAudio(message *Message) {
	// mediaHeader에서 직접 정보 추출 (제로카피)
	firstByte := message.mediaHeader[0] // Audio Info

	// 코덱 동적 감지
	codecType, err := detectAudioCodec(firstByte)
	if err != nil {
		slog.Error("Unsupported audio codec", "sessionId", s.ID(), "firstByte", firstByte, "err", err)
		return
	}

	frameType := s.parseAudioFrameType(firstByte, message.mediaHeader)

	// 순수 데이터를 버퍼 배열로 변환 (zero-copy)
	frameData := make([]*media.Buffer, len(message.payloads))
	for i, buffer := range message.payloads {
		frameData[i] = buffer.AddRef() // 참조 카운트 증가
	}
	
	// Packet 생성 (오디오는 트랙 1)
	trackIndex := 1
	packet := media.NewPacket(trackIndex, codecType, media.FormatRawStream, frameType, uint64(message.messageHeader.timestamp), 0, frameData)

	// message의 streamID에 해당하는 스트림에 전송
	if stream, exists := s.publishedStreams[message.messageHeader.streamID]; exists {
		// 트랙이 없으면 추가
		if stream.TrackCount() <= trackIndex {
			stream.AddTrack(packet.Codec, media.TimeScaleRTMP)
		}

		stream.SendPacket(packet)
	}
}

// 비디오 데이터 처리
func (s *session) handleVideo(message *Message) {
	// mediaHeader에서 직접 정보 추출 (제로카피)
	firstByte := message.mediaHeader[0] // Frame Type + Codec ID

	// 코덱 동적 감지
	codecType, err := detectVideoCodec(firstByte)
	if err != nil {
		slog.Error("Unsupported video codec", "sessionId", s.ID(), "firstByte", firstByte, "err", err)
		return
	}

	frameType := s.parseVideoFrameType(firstByte, message.mediaHeader)

	// 비디오 헤더에서 CompositionTime 추출
	_, _, _, compositionTime := ParseVideoHeader(message.mediaHeader)

	// 순수 데이터를 버퍼 배열로 변환 (zero-copy)
	frameData := make([]*media.Buffer, len(message.payloads))
	for i, buffer := range message.payloads {
		frameData[i] = buffer.AddRef() // 참조 카운트 증가
	}
	

	// Packet 생성 (비디오는 트랙 0)
	trackIndex := 0
	packet := media.NewPacket(trackIndex, codecType, media.FormatH26xAVCC, frameType, uint64(message.messageHeader.timestamp), compositionTime, frameData)

	// message의 streamID에 해당하는 스트림에 전송
	if stream, exists := s.publishedStreams[message.messageHeader.streamID]; exists {
		// 트랙이 없으면 추가
		if stream.TrackCount() <= trackIndex {
			stream.AddTrack(packet.Codec, media.TimeScaleRTMP)
		}

		var totalLen int
		for _, buffer := range frameData {
			totalLen += len(buffer.Data())
		}
		stream.SendPacket(packet)
	} else {
		slog.Warn("Stream not found", "streamID", message.messageHeader.streamID)
	}
}

// parseAudioFrameType 오디오 프레임 타입 파싱
func (s *session) parseAudioFrameType(firstByte byte, payload []byte) media.PacketType {
	// AAC 특수 처리
	if ((firstByte>>4)&0x0F) == 10 && len(payload) > 1 {
		switch payload[1] {
		case 0:
			return media.TypeConfig
		case 1:
			return media.TypeData
		}
	}

	// 기본적으로는 raw 오디오로 처리
	return media.TypeData
}

// parseVideoFrameType 비디오 프레임 타입 파싱
func (s *session) parseVideoFrameType(firstByte byte, payload []byte) media.PacketType {
	// 프레임 타입 (4비트)
	frameTypeFlag := (firstByte >> 4) & 0x0F
	codecId := firstByte & 0x0F

	// H.264 특수 처리
	if codecId == 7 && len(payload) > 1 {
		avcPacketType := payload[1]
		switch avcPacketType {
		case 0:
			return media.TypeConfig
		case 1:
			if frameTypeFlag == 1 {
				return media.TypeKey
			}
			return media.TypeData
		case 2:
			// RTMP 전용 EndOfSequence는 키프레임으로 처리
			return media.TypeKey
		}
	}

	// 일반적인 프레임 타입
	switch frameTypeFlag {
	case 1:
		return media.TypeKey
	case 2:
		return media.TypeData
	case 3:
		return media.TypeData
	case 4:
		return media.TypeKey
	case 5:
		// RTMP 전용 InfoFrame은 인터프레임으로 처리
		return media.TypeData
	default:
		return media.TypeData
	}
}

// handleAMF0ScriptData 스크립트 데이터 처리 (메타데이터 등)
func (s *session) handleAMF0ScriptData(message *Message) {

	// AMF 데이터 디코딩 (zero-copy 최적화)
	reader := message.Reader()
	values, err := amf.DecodeAMF0Sequence(reader)
	if err != nil {
		slog.Error("failed to decode script data", "err", err)
		return
	}

	if len(values) == 0 {
		slog.Warn("empty script data")
		return
	}

	// 첫 번째 값은 보통 명령어 이름
	commandName, ok := values[0].(string)
	if !ok {
		slog.Error("invalid script command name", "type", utils.TypeName(values[0]))
		return
	}

	switch commandName {
	case "onMetaData":
		s.handleOnMetaData(values)
	default:
		slog.Error("unknown script command", "command", commandName, "values", values)
	}
}

// handleOnMetaData 메타데이터 처리
func (s *session) handleOnMetaData(values []any) {

	if len(values) < 2 {
		slog.Warn("onMetaData: insufficient data")
		return
	}

	if !s.IsPublisher() {
		slog.Warn("received metadata but not publishing")
		return
	}

	// 발행 중인 스트림이 없으면 경고
	if len(s.publishedStreams) == 0 {
		slog.Warn("received metadata but no published streams")
		return
	}

	// 두 번째 값은 메타데이터 객체
	metadata, ok := values[1].(map[string]any)
	if !ok {
		slog.Error("onMetaData: invalid metadata object", "type", utils.TypeName(values[1]))
		return
	}

	// 모든 발행된 스트림에 메타데이터 전송
	if len(s.publishedStreams) > 0 {
		// any 값을 string으로 변환
		stringMetadata := make(map[string]string)
		for k, v := range metadata {
			stringMetadata[k] = fmt.Sprintf("%v", v)
		}

		// 모든 발행된 스트림에 전송
		for _, stream := range s.publishedStreams {
			stream.SendMetadata(stringMetadata)
		}
	} else {
		slog.Warn("No published streams for metadata", "sessionId", s.ID())
	}
}

// GetStreamInfo 세션 정보를 반환 (다중 스트림 지원)
func (s *session) GetStreamInfo() (streamCount int, isPublishing bool, isPlaying bool) {
	return len(s.publishedStreams), s.isPublishingMode(), false // isPlaying은 항상 false (MediaServer에서 관리)
}

// 구현을 위한 헬퍼 메서드들

// sendMetadataToClient 메타데이터를 RTMP onMetaData 메시지로 전송
func (s *session) sendMetadataToClient(metadata map[string]string, streamID uint32) error {
	// string을 any map으로 변환 (AMF 인코딩을 위해)
	interfaceMetadata := make(map[string]any)
	for k, v := range metadata {
		interfaceMetadata[k] = v
	}

	// AMF0 onMetaData 시퀀스로 인코딩
	values := []any{"onMetaData", interfaceMetadata}

	// AMF0 시퀀스를 바이트로 인코딩
	encodedData, err := amf.EncodeAMF0Sequence(values...)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	// script data 메시지로 전송
	message := &Message{
		messageHeader: &messageHeader{
			timestamp: 0,
			length:    uint32(len(encodedData)),
			typeId:    MsgTypeAMF0Data,
			streamID:  streamID,
		},
		payloads: func() []*media.Buffer {
			buffer := media.NewBuffer(len(encodedData))
			copy(buffer.Data(), encodedData)
			return []*media.Buffer{buffer}
		}(),
	}

	err = s.writer.writeMessage(s.bufReadWriter, message)
	if err == nil {
		s.bufReadWriter.Flush()
	}
	message.Release() // 전송 완료 후 메모리 해제
	return err
}

// calculateDataSize 데이터 크기 계산 헬퍼 함수
func (s *session) calculateDataSize(data []byte) uint32 {
	return uint32(len(data))
}


// handleSetChunkSize 청크 크기 설정 처리
func (s *session) handleSetChunkSize(message *Message) {
	slog.Debug("handleSetChunkSize", "sessionId", s.ID())

	// Zero-copy 시도: 단일 버퍼이고 정확히 4바이트인 경우
	if newChunkSize, ok := message.GetUint32FromPayload(); ok {
		// Zero-copy 성공 - 직접 처리
		s.processChunkSizeValue(newChunkSize)
		return
	}

	// Fallback: 복잡한 경우 기존 방식 사용
	payload := message.Payload()
	if len(payload) != 4 {
		slog.Error("Invalid Set Chunk Size message length", "length", len(payload), "sessionId", s.ID())
		return
	}

	newChunkSize := binary.BigEndian.Uint32(payload[:4])
	s.processChunkSizeValue(newChunkSize)
}

// processChunkSizeValue 청크 크기 값 처리 (공통 로직)
func (s *session) processChunkSizeValue(newChunkSize uint32) {

	// 첫 번째 비트(최상위 비트) 체크: 반드시 0이어야 함
	if newChunkSize&0x80000000 != 0 {
		slog.Error("Set Chunk Size has reserved highest bit set", "value", newChunkSize, "sessionId", s.ID())
		return
	}

	// RTMP 최대 청크 크기 제한 (1 ~ 16777215)
	if newChunkSize < 1 || newChunkSize > ExtendedTimestampThreshold {
		slog.Error("Set Chunk Size out of valid range", "value", newChunkSize, "sessionId", s.ID())
		return
	}

	// 실제 세션 청크 크기 적용
	s.reader.setChunkSize(newChunkSize)
}

// handleAbort 청크 스트림 중단 처리
func (s *session) handleAbort(message *Message) {
	slog.Debug("handleAbort", "sessionId", s.ID())

	// Zero-copy 시도: 단일 버퍼이고 정확히 4바이트인 경우
	if chunkStreamId, ok := message.GetUint32FromPayload(); ok {
		// Zero-copy 성공 - 직접 처리
		s.reader.abortChunkStream(chunkStreamId)
		slog.Info("Chunk stream aborted", "chunkStreamId", chunkStreamId, "sessionId", s.ID())
		return
	}

	// Fallback: 복잡한 경우 기존 방식 사용
	payload := message.Payload()
	if len(payload) != 4 {
		slog.Error("Invalid Abort message length", "length", len(payload), "sessionId", s.ID())
		return
	}

	chunkStreamId := binary.BigEndian.Uint32(payload[:4])
	
	// Reader에서 해당 청크 스트림 상태 초기화
	s.reader.abortChunkStream(chunkStreamId)

	slog.Info("Chunk stream aborted", "chunkStreamId", chunkStreamId, "sessionId", s.ID())
}

// handleAMF0Command AMF0 명령어 처리
func (s *session) handleAMF0Command(message *Message) {
	slog.Debug("handleAMFCommand", "sessionId", s.ID())
	reader := message.Reader()
	values, err := amf.DecodeAMF0Sequence(reader)
	if err != nil {
		slog.Error("Failed to decode AMF sequence", "sessionId", s.ID(), "err", err)
		return
	}

	if len(values) == 0 {
		slog.Error("Empty AMF sequence", "sessionId", s.ID())
		return
	}

	commandName, ok := values[0].(string)
	if !ok {
		slog.Error("Invalid command name type", "sessionId", s.ID(), "actual", utils.TypeName(values[0]))
		return
	}

	switch commandName {
	// 연결 관련
	case "connect":
		s.handleConnect(values)
	case "createStream":
		s.handleCreateStream(values)
	// 발행 관련
	case "publish":
		s.handlePublish(message, values)
	case "FCPublish":
		s.handleFCPublish(values)
	case "releaseStream":
		s.handleReleaseStream(values)
	// 재생 관련
	case "play":
		s.handlePlay(message, values)
	// 종료 관련
	case "FCUnpublish":
		s.handleFCUnpublish(message, values)
	case "closeStream":
		s.handleCloseStream(message, values)
	case "deleteStream":
		s.handleDeleteStream(message, values)
	// 기타
	case "getStreamLength":
		s.handleGetStreamLength(values)
	default:
		slog.Error("Unknown AMF command", "sessionId", s.ID(), "name", commandName)
	}
}

// Command handler 함수들 (switch case 순서와 동일하게 배치)

// handleConnect connect 명령어 처리
func (s *session) handleConnect(values []any) {

	// 최소 3개 요소: "connect", transaction ID, command object
	if len(values) < 3 {
		slog.Error("connect: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("connect: invalid transaction ID", "type", utils.TypeName(values[1]))
		return
	}

	// command object (map)
	commandObj, ok := values[2].(map[string]any)
	if !ok {
		slog.Error("connect: invalid command object", "type", utils.TypeName(values[2]))
		return
	}

	// app 이름 추출
	if app, ok := commandObj["app"]; ok {
		if appName, ok := app.(string); ok {
			s.appName = appName
			}
	}

	obj := map[string]any{
		"level":          "status",
		"code":           "NetConnection.Connect.Success",
		"description":    "Connection succeeded.",
		"objectEncoding": 0,
	}

	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, obj)
	if err != nil {
		return
	}

	err = s.writer.writeSetChunkSize(s.bufReadWriter, 4096)
	if err != nil {
		return
	}

	// 서버 측에서도 청크 크기 설정 (들어오는 데이터 처리용)
	s.reader.setChunkSize(4096)

	err = s.writer.writeCommand(s.bufReadWriter, sequence)
	if err != nil {
		return
	}
	
	// 명령어 전송 후 플러시
	s.bufReadWriter.Flush()
}

// handleCreateStream createStream 명령어 처리
func (s *session) handleCreateStream(values []any) {

	if len(values) < 2 {
		slog.Error("createStream: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("createStream: invalid transaction ID", "type", utils.TypeName(values[1]))
		return
	}

	// 새로운 스트림 ID 생성 (실제 사용할 streamID)
	newStreamID := s.generateStreamID()

	// _result 응답 전송
	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, float64(newStreamID))
	if err != nil {
		slog.Error("createStream: failed to encode response", "err", err)
		return
	}

	err = s.writer.writeCommand(s.bufReadWriter, sequence)
	if err != nil {
		slog.Error("createStream: failed to write response", "err", err)
		return
	}
	
	// 응답 전송 후 플러시
	s.bufReadWriter.Flush()

}

// handlePublish publish 명령어 처리 (소스 모드 활성화)
func (s *session) handlePublish(message *Message, values []any) {

	if len(values) < 3 {
		slog.Error("publish: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("publish: invalid transaction ID", "type", utils.TypeName(values[1]))
		return
	}

	// 스트림 이름
	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("publish: invalid stream name", "type", utils.TypeName(values[3]))
		return
	}

	// 발행 유형 (옵션널) - 현재 사용하지 않음
	_ = "live" // 기본값
	if len(values) > 4 {
		if pt, ok := values[4].(string); ok {
			_ = pt
		}
	}

	fullStreamPath := s.appName + "/" + streamName
	if s.appName == "" || streamName == "" {
		slog.Error("publish: invalid stream path", "appName", s.appName, "streamKey", streamName)
		return
	}


	// 1단계: message의 streamID 사용 및 스트림 준비
	streamID := message.messageHeader.streamID
	stream := media.NewStream(fullStreamPath)
	s.addPublishedStream(streamID, fullStreamPath, stream)

	// 2단계: MediaServer에 실제 publish 시도 (collision detection + 원자적 점유)
	if !s.attemptStreamPublish(fullStreamPath, stream) {
		// 실패 시 정리
		s.removePublishedStream(streamID)
		s.sendPublishErrorResponse(transactionID, "NetStream.Publish.BadName", "Stream key was taken by another client")
		return
	}

	// 3단계: 성공 응답 전송
	s.sendPublishSuccessResponse(transactionID, fullStreamPath)

}

// handleFCPublish Flash Media Server 호환 명령어 처리
func (s *session) handleFCPublish(_ []any) {
	slog.Debug("FCPublish command received (ignored)", "sessionId", s.ID())
}

// handleReleaseStream Flash Media Server 호환 명령어 처리
func (s *session) handleReleaseStream(_ []any) {
	slog.Debug("releaseStream command received (ignored)", "sessionId", s.ID())
}

// handlePlay play 명령어 처리 (싱크 모드 활성화)
func (s *session) handlePlay(message *Message, values []any) {
	streamID := message.messageHeader.streamID


	if len(values) < 3 {
		slog.Error("play: not enough parameters", "length", len(values))
		return
	}

	_, ok := values[1].(float64)
	if !ok {
		slog.Error("play: invalid transaction ID", "type", utils.TypeName(values[1]))
		return
	}

	// 스트림 이름
	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("play: invalid stream name", "type", utils.TypeName(values[3]))
		return
	}

	fullStreamPath := s.appName + "/" + streamName
	if s.appName == "" || streamName == "" {
		slog.Error("play: invalid stream path", "appName", s.appName, "streamKey", streamName)
		return
	}


	// 이미 구독 중인지 체크
	for existingStreamId, existingPath := range s.subscribedStreams {
		if existingPath == fullStreamPath {
			slog.Warn("Already subscribed to stream", "sessionId", s.ID(), "streamID", existingStreamId, "streamPath", fullStreamPath)
		}
	}

	// 먼저 구독 스트림 매핑을 설정 (MediaServer 신호 보내기 전에)
	s.addSubscribedStream(streamID, fullStreamPath)

	// MediaServer에 subscribe 시작 알림 및 응답 대기
	responseChan := make(chan media.Response, 1)

	// RTMP가 지원하는 코덱 목록
	supportedCodecs := []media.Codec{media.H264, media.AAC}

	// MediaServer에 이벤트 전송 (버퍼가 있어서 거의 항상 성공)
	s.mediaServerChannel <- media.NewSubscribeStarted(s.ID(), fullStreamPath, supportedCodecs, responseChan)

	// 응답 대기
	select {
	case response := <-responseChan:
		if !response.Success {
			slog.Error("Subscribe attempt failed", "sessionId", s.ID(), "streamPath", fullStreamPath, "error", response.Error)
			// 실패 시 미리 추가한 매핑 제거
			s.removeSubscribedStream(streamID)
			// NetStream.Play.Failed 전송
			failedStatusObj := map[string]any{
				"level":       "error",
				"code":        "NetStream.Play.Failed",
				"description": response.Error,
			}
			if failedSequence, err := amf.EncodeAMF0Sequence("onStatus", 0.0, nil, failedStatusObj); err == nil {
				s.writer.writeCommand(s.bufReadWriter, failedSequence)
				s.bufReadWriter.Flush()
			}
			return
		}
	case <-s.ctx.Done():
		slog.Error("Subscribe attempt cancelled", "sessionId", s.ID(), "streamPath", fullStreamPath)
		// 취소 시 미리 추가한 매핑 제거
		s.removeSubscribedStream(streamID)
		return
	}

	// 1. NetStream.Play.Reset 전송
	resetStatusObj := map[string]any{
		"level":       "status",
		"code":        "NetStream.Play.Reset",
		"description": fmt.Sprintf("Resetting and playing stream %s", fullStreamPath),
		"details":     fullStreamPath,
	}

	resetSequence, err := amf.EncodeAMF0Sequence("onStatus", 0.0, nil, resetStatusObj)
	if err != nil {
		slog.Error("play: failed to encode reset onStatus", "err", err)
		return
	}

	err = s.writer.writeCommand(s.bufReadWriter, resetSequence)
	if err != nil {
		slog.Error("play: failed to write reset onStatus", "err", err)
		return
	}

	// 2. NetStream.Play.Start 전송
	startStatusObj := map[string]any{
		"level":       "status",
		"code":        "NetStream.Play.Start",
		"description": fmt.Sprintf("Started playing stream %s", fullStreamPath),
		"details":     fullStreamPath,
	}

	startSequence, err := amf.EncodeAMF0Sequence("onStatus", 0.0, nil, startStatusObj)
	if err != nil {
		slog.Error("play: failed to encode start onStatus", "err", err)
		return
	}

	err = s.writer.writeCommand(s.bufReadWriter, startSequence)
	if err != nil {
		slog.Error("play: failed to write start onStatus", "err", err)
		return
	}
	
	// 최종 응답 전송 후 플러시
	s.bufReadWriter.Flush()

}

// stopPublishing 발행 중단 처리
func (s *session) stopPublishing() {
	// 스트림 발행 중단 처리
	for _, stream := range s.publishedStreams {
		s.mediaServerChannel <- media.NewPublishStopped(s.ID(), stream.ID())
	}

	// 스트림 정리
	s.publishedStreams = make(map[uint32]*media.Stream)
	s.streamIDToPath = make(map[uint32]string)
	s.streamPathToId = make(map[string]uint32)
}

// stopSubscribing 구독 중단 처리
func (s *session) stopSubscribing() {
	// 구독 중인 스트림에 대해 재생 중단 이벤트 전송
	for _, streamPath := range s.subscribedStreams {
		s.mediaServerChannel <- media.NewSubscribeStopped(s.ID(), streamPath)
	}

	// 구독 스트림 정리
	s.subscribedStreams = make(map[uint32]string)
}

// handleAMF3ScriptData AMF3 스크립트 데이터 처리
func (s *session) handleAMF3ScriptData(message *Message) {
	// AMF3 데이터 디코딩 (zero-copy 최적화)
	reader := message.Reader()

	// AMF3 컨텍스트를 사용하여 디코딩
	values, err := amf.DecodeAMF3Sequence(reader)
	if err != nil {
		slog.Error("failed to decode AMF3 script data", "sessionId", s.ID(), "err", err)
		return
	}

	if len(values) == 0 {
		slog.Warn("empty AMF3 script data", "sessionId", s.ID())
		return
	}

	// 첫 번째 값이 "onMetaData"인지 확인
	if len(values) >= 2 {
		if cmd, ok := values[0].(string); ok && cmd == "onMetaData" {
			slog.Debug("received AMF3 onMetaData", "sessionId", s.ID())
			// 메타데이터를 모든 발행된 스트림으로 전달
			if len(s.publishedStreams) > 0 {
				if metadata, ok := values[1].(map[string]any); ok {
					stringMetadata := make(map[string]string)
					for k, v := range metadata {
						stringMetadata[k] = fmt.Sprintf("%v", v)
					}
					for _, stream := range s.publishedStreams {
						stream.SendMetadata(stringMetadata)
					}
				}
			}
		}
	}
}

// handleAMF3Command AMF3 커맨드 처리
func (s *session) handleAMF3Command(message *Message) {
	slog.Debug("handleAMF3Command", "sessionId", s.ID())

	reader := message.Reader()

	// AMF3 컨텍스트를 사용하여 디코딩
	values, err := amf.DecodeAMF3Sequence(reader)
	if err != nil {
		slog.Error("Failed to decode AMF3 sequence", "sessionId", s.ID(), "err", err)
		return
	}

	if len(values) == 0 {
		slog.Error("Empty AMF3 sequence", "sessionId", s.ID())
		return
	}

	// 첫 번째 값이 커맨드 이름
	commandName, ok := values[0].(string)
	if !ok {
		slog.Error("Invalid AMF3 command name", "sessionId", s.ID(), "type", utils.TypeName(values[0]))
		return
	}

	slog.Debug("AMF3 command received", "sessionId", s.ID(), "command", commandName)

	switch commandName {
	// 연결 관련
	case "connect":
		s.handleConnect(values)
	case "createStream":
		s.handleCreateStream(values)
	// 발행 관련
	case "publish":
		s.handlePublish(message, values)
	case "FCPublish":
		s.handleFCPublish(values)
	case "releaseStream":
		s.handleReleaseStream(values)
	// 재생 관련
	case "play":
		s.handlePlay(message, values)
	// 종료 관련
	case "FCUnpublish":
		s.handleFCUnpublish(message, values)
	case "closeStream":
		s.handleCloseStream(message, values)
	case "deleteStream":
		s.handleDeleteStream(message, values)
	// 기타
	case "getStreamLength":
		s.handleGetStreamLength(values)
	default:
		slog.Error("Unsupported AMF3 command", "sessionId", s.ID(), "command", commandName)
	}
}

// --- 다중 스트림 관리 메서드들 ---

// addPublishedStream 발행 스트림 추가 (streamID 기반)
func (s *session) addPublishedStream(streamID uint32, streamPath string, stream *media.Stream) {
	s.publishedStreams[streamID] = stream
	s.addStreamMapping(streamID, streamPath)
	slog.Debug("Published stream added", "sessionId", s.ID(), "streamID", streamID, "streamPath", streamPath, "mediaStreamId", stream.ID())
}

// removePublishedStream 발행 스트림 제거 (streamID 기반)
func (s *session) removePublishedStream(streamID uint32) *media.Stream {
	stream, exists := s.publishedStreams[streamID]
	if exists {
		delete(s.publishedStreams, streamID)
		s.removeStreamMapping(streamID)
		slog.Debug("Published stream removed", "sessionId", s.ID(), "streamID", streamID)
	}
	return stream
}

// getPublishedStream 발행 스트림 가져오기 (streamID 기반)
func (s *session) getPublishedStream(streamID uint32) (*media.Stream, bool) {
	stream, exists := s.publishedStreams[streamID]
	return stream, exists
}

// getAllPublishedStreams 모든 발행 스트림 가져오기
func (s *session) getAllPublishedStreams() []*media.Stream {
	streams := make([]*media.Stream, 0, len(s.publishedStreams))
	for _, stream := range s.publishedStreams {
		streams = append(streams, stream)
	}
	return streams
}

// addSubscribedStream 구독 스트림 추가
func (s *session) addSubscribedStream(streamID uint32, streamPath string) {
	s.subscribedStreams[streamID] = streamPath
	s.streamPathToId[streamPath] = streamID // 역방향 매핑도 추가
	slog.Debug("Subscribed stream added", "sessionId", s.ID(), "streamID", streamID, "streamPath", streamPath)
}

// removeSubscribedStream 구독 스트림 제거
func (s *session) removeSubscribedStream(streamID uint32) {
	if streamPath, exists := s.subscribedStreams[streamID]; exists {
		delete(s.subscribedStreams, streamID)
		delete(s.streamPathToId, streamPath) // 역방향 매핑도 제거
		slog.Debug("Subscribed stream removed", "sessionId", s.ID(), "streamID", streamID, "streamPath", streamPath)
	}
}

// clearSubscribedStreams 모든 구독 스트림 제거
func (s *session) clearSubscribedStreams() {
	s.subscribedStreams = make(map[uint32]string)
	s.streamPathToId = make(map[string]uint32) // 역방향 매핑도 초기화
	slog.Debug("All subscribed streams cleared", "sessionId", s.ID())
}

// isPublishingMode 발행 모드 여부 확인
func (s *session) isPublishingMode() bool {
	return len(s.publishedStreams) > 0
}

// --- MediaSource 인터페이스 구현 (발행자 모드) ---

// PublishingStreams MediaSource 인터페이스 구현 - 발행 중인 스트림 목록 반환
func (s *session) PublishingStreams() []*media.Stream {
	return s.getAllPublishedStreams()
}

// SubscribedStreams MediaSink 인터페이스 구현 - 구독 중인 스트림 ID 목록 반환
func (s *session) SubscribedStreams() []string {
	if len(s.subscribedStreams) > 0 {
		result := make([]string, 0, len(s.subscribedStreams))
		for _, streamPath := range s.subscribedStreams {
			result = append(result, streamPath)
		}
		return result
	}
	return nil
}

// --- 스트림 ID 관리 헬퍼 메서드들 ---

// generateStreamID 새로운 streamID 생성
func (s *session) generateStreamID() int {
	streamID := s.nextStreamId
	s.nextStreamId++
	return streamID
}

// addStreamMapping streamID와 streamPath 매핑 추가
func (s *session) addStreamMapping(streamID uint32, streamPath string) {
	s.streamIDToPath[streamID] = streamPath
	s.streamPathToId[streamPath] = streamID
}

// removeStreamMapping streamID 매핑 제거
func (s *session) removeStreamMapping(streamID uint32) {
	if streamPath, exists := s.streamIDToPath[streamID]; exists {
		delete(s.streamIDToPath, streamID)
		delete(s.streamPathToId, streamPath)
	}
}

// --- StreamKey collision detection 메서드 ---

// attemptStreamPublish MediaServer에 publish 시도 (collision detection + 원자적 점유)
func (s *session) attemptStreamPublish(streamKey string, stream *media.Stream) bool {
	// MediaServer에 publish 시도 요청
	responseChan := make(chan media.Response, 1)
	publishAttempt := media.NewPublishStarted(s.ID(), stream, responseChan)

	// MediaServer에 이벤트 전송 (버퍼가 있어서 거의 항상 성공)
	s.mediaServerChannel <- publishAttempt

	// 응답 대기
	select {
	case response := <-responseChan:
		if response.Success {
				return true
		} else {
			slog.Error("Stream publish attempt failed", "sessionId", s.ID(), "streamKey", streamKey, "error", response.Error)
			return false
		}
	case <-s.ctx.Done():
		slog.Error("Stream publish cancelled", "sessionId", s.ID(), "streamKey", streamKey)
		return false
	}
}

// sendPublishErrorResponse publish 에러 응답 전송
func (s *session) sendPublishErrorResponse(_ float64, code string, description string) {
	statusObj := map[string]any{
		"level":       "error",
		"code":        code,
		"description": description,
	}

	statusSequence, err := amf.EncodeAMF0Sequence("onStatus", 0.0, nil, statusObj)
	if err != nil {
		slog.Error("Failed to encode publish error response", "sessionId", s.ID(), "err", err)
		return
	}

	err = s.writer.writeCommand(s.bufReadWriter, statusSequence)
	if err != nil {
		slog.Error("Failed to write publish error response", "sessionId", s.ID(), "err", err)
	} else {
		s.bufReadWriter.Flush()
	}
}

// sendPublishSuccessResponse publish 성공 응답 전송
func (s *session) sendPublishSuccessResponse(_ float64, streamPath string) {
	statusObj := map[string]any{
		"level":       "status",
		"code":        "NetStream.Publish.Start",
		"description": fmt.Sprintf("Started publishing stream %s", streamPath),
		"details":     streamPath,
	}

	statusSequence, err := amf.EncodeAMF0Sequence("onStatus", 0.0, nil, statusObj)
	if err != nil {
		slog.Error("Failed to encode publish success response", "sessionId", s.ID(), "err", err)
		return
	}

	err = s.writer.writeCommand(s.bufReadWriter, statusSequence)
	if err != nil {
		slog.Error("Failed to write publish success response", "sessionId", s.ID(), "err", err)
	} else {
		s.bufReadWriter.Flush()
	}
}

// handleFCUnpublish FCUnpublish 명령어 처리
func (s *session) handleFCUnpublish(message *Message, _ []any) {
	streamID := message.messageHeader.streamID

	// 특정 streamID의 발행 스트림만 정리
	if _, exists := s.publishedStreams[streamID]; exists {
		s.removePublishedStream(streamID)
		slog.Info("FCUnpublish: stopped specific stream", "sessionId", s.ID(), "streamID", streamID)
	} else {
		slog.Debug("FCUnpublish: no stream found for streamID", "sessionId", s.ID(), "streamID", streamID)
	}
}

// handleCloseStream closeStream 명령어 처리
func (s *session) handleCloseStream(message *Message, _ []any) {
	streamID := message.messageHeader.streamID

	// 특정 streamID의 발행 스트림 정리
	if _, exists := s.publishedStreams[streamID]; exists {
		s.removePublishedStream(streamID)
		slog.Info("closeStream: stopped publishing stream", "sessionId", s.ID(), "streamID", streamID)
	}

	// 특정 streamID의 재생 스트림 정리
	if streamPath, exists := s.subscribedStreams[streamID]; exists {
		s.removeSubscribedStream(streamID)
		slog.Info("closeStream: stopped subscribing stream", "sessionId", s.ID(), "streamID", streamID, "streamPath", streamPath)
	}

	slog.Info("closeStream: processed stream close", "sessionId", s.ID(), "streamID", streamID)
}

// handleDeleteStream deleteStream 명령어 처리
func (s *session) handleDeleteStream(message *Message, _ []any) {
	streamID := message.messageHeader.streamID

	// 특정 streamID의 발행 스트림 정리
	if _, exists := s.publishedStreams[streamID]; exists {
		s.removePublishedStream(streamID)
		slog.Info("deleteStream: stopped publishing stream", "sessionId", s.ID(), "streamID", streamID)
	}

	// 특정 streamID의 재생 스트림 정리
	if streamPath, exists := s.subscribedStreams[streamID]; exists {
		s.removeSubscribedStream(streamID)
		slog.Info("deleteStream: stopped subscribing stream", "sessionId", s.ID(), "streamID", streamID, "streamPath", streamPath)
	}

	slog.Info("deleteStream: processed stream deletion", "sessionId", s.ID(), "streamID", streamID)
}

// handleGetStreamLength getStreamLength 명령어 처리 (Flash Media Server 호환)
func (s *session) handleGetStreamLength(values []any) {
	// getStreamLength는 보통 최소 3개 매개변수: "getStreamLength", transactionID, null, streamName
	if len(values) < 4 {
		slog.Debug("getStreamLength: insufficient parameters", "sessionId", s.ID(), "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Debug("getStreamLength: invalid transaction ID", "sessionId", s.ID(), "type", utils.TypeName(values[1]))
		return
	}

	// 스트림 이름 (4번째 매개변수)
	streamName, ok := values[3].(string)
	if !ok {
		slog.Debug("getStreamLength: invalid stream name", "sessionId", s.ID(), "type", utils.TypeName(values[3]))
		return
	}

	slog.Debug("getStreamLength request", "sessionId", s.ID(), "streamName", streamName, "transactionID", transactionID)

	// 라이브 스트림의 경우 길이는 항상 -1 (무한대/라이브)을 반환
	// 이는 Flash Media Server의 표준 동작입니다
	streamLength := -1.0

	// _result 응답 전송
	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, streamLength)
	if err != nil {
		slog.Error("getStreamLength: failed to encode response", "sessionId", s.ID(), "err", err)
		return
	}

	err = s.writer.writeCommand(s.bufReadWriter, sequence)
	if err != nil {
		slog.Error("getStreamLength: failed to write response", "sessionId", s.ID(), "err", err)
		return
	}
	
	// 응답 전송 후 플러시
	s.bufReadWriter.Flush()

	slog.Debug("getStreamLength response sent", "sessionId", s.ID(), "streamName", streamName, "length", streamLength, "transactionID", transactionID)
}
