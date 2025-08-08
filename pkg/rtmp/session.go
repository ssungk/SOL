package rtmp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/amf"
	"sol/pkg/media"
	"sol/pkg/utils"
	"sync"
	"unsafe"
)

// session RTMP에서 사용하는 세션 구조체 (MediaSource/MediaSink 직접 구현)
type session struct {
	reader *messageReader
	writer *messageWriter
	conn   net.Conn

	// 세션 식별자 - 포인터 주소값 기반
	sessionId string

	// 스트림 관리
	streamID   uint32
	streamName string // streamkey
	appName    string // appname
	stream     *media.Stream

	// MediaServer로 직접 이벤트 전송
	mediaServerChannel chan<- any

	// 상태 관리 (동시성 안전성을 위한 뮤텍스)
	mu           sync.RWMutex
	isPublishing bool
	isPlaying    bool
	isActive     bool
}

// 세션의 ID를 반환 (sessionId 필드)
func (s *session) GetID() string {
	return s.sessionId
}

// 인터페이스 구현

// 노드 고유 ID 반환 (MediaNode 인터페이스)
func (s *session) ID() uintptr {
	if s == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(s))
}

// 노드 타입 반환 (MediaNode 인터페이스)
func (s *session) MediaType() media.MediaNodeType {
	return media.MediaNodeTypeRTMP
}

// 연결 주소 반환 (MediaNode 인터face)
func (s *session) Address() string {
	if s.conn != nil {
		return s.conn.RemoteAddr().String()
	}
	return ""
}

// 세션 시작 (MediaNode 인터페이스)
func (s *session) Start() error {
	s.mu.Lock()
	s.isActive = true
	s.mu.Unlock()
	slog.Info("RTMP session started", "sessionId", s.sessionId)
	return nil
}

// 세션 중지 (MediaNode 인터페이스)
func (s *session) Stop() error {
	s.mu.Lock()
	s.isActive = false
	s.mu.Unlock()
	s.cleanup()
	slog.Info("RTMP session stopped", "sessionId", s.sessionId)
	return nil
}

// 인터페이스 구현 (플레이어 모드일 때)

// 미디어 프레임 전송 (MediaSink 인터페이스)
func (s *session) SendMediaFrame(streamId string, frame media.Frame) error {
	s.mu.RLock()
	playing := s.isPlaying
	active := s.isActive
	s.mu.RUnlock()

	if !playing || !active || s.conn == nil {
		return fmt.Errorf("session not playing or inactive")
	}

	// pkg/media Frame을 RTMP 메시지로 변환하여 전송
	var err error
	switch frame.Type {
	case media.TypeVideo:
		err = s.sendVideoFrame(frame)
	case media.TypeAudio:
		err = s.sendAudioFrame(frame)
	default:
		slog.Warn("Unknown frame type", "type", frame.Type, "sessionId", s.sessionId)
		return nil
	}

	if err != nil {
		slog.Error("Failed to send frame to RTMP session", "sessionId", s.sessionId, "streamId", streamId, "subType", frame.SubType, "err", err)
		return err
	}

	slog.Debug("Media frame sent to RTMP session", "sessionId", s.sessionId, "streamId", streamId, "subType", frame.SubType, "timestamp", frame.Timestamp)

	return nil
}

// 메타데이터 전송 (MediaSink 인터페이스)
func (s *session) SendMetadata(streamId string, metadata map[string]string) error {
	if !s.isPlaying || !s.isActive || s.conn == nil {
		return fmt.Errorf("session not playing or inactive")
	}

	// pkg/media metadata를 RTMP onMetaData 메시지로 변환하여 전송
	err := s.sendMetadataToClient(metadata)
	if err != nil {
		slog.Error("Failed to send metadata to RTMP session", "sessionId", s.sessionId, "streamId", streamId, "err", err)
		return err
	}

	slog.Info("Metadata sent to RTMP session", "sessionId", s.sessionId, "streamId", streamId, "metadataKeys", len(metadata))

	return nil
}

// 스트림 설정 (발행자 모드)
func (s *session) SetStream(stream *media.Stream) {
	s.stream = stream
	slog.Info("Stream set for RTMP session", "sessionId", s.sessionId, "streamId", stream.GetId())
}

// 연결된 스트림 반환
func (s *session) GetStream() *media.Stream {
	return s.stream
}

// 발행자 여부 확인
func (s *session) IsPublisher() bool {
	return s.isPublishing
}

// 플레이어 여부 확인
func (s *session) IsPlayer() bool {
	return s.isPlaying
}

// createStream 명령어 처리
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

	// 새로운 스트림 ID 생성 (1부터 시작)
	s.streamID = 1

	// _result 응답 전송
	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, float64(s.streamID))
	if err != nil {
		slog.Error("createStream: failed to encode response", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, sequence)
	if err != nil {
		slog.Error("createStream: failed to write response", "err", err)
		return
	}

	slog.Info("createStream successful", "streamID", s.streamID, "transactionID", transactionID)
}

// publish 명령어 처리 (소스 모드 활성화)
func (s *session) handlePublish(values []any) {

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

	// 발행 유형 (옵션널)
	publishType := "live" // 기본값
	if len(values) > 4 {
		if pt, ok := values[4].(string); ok {
			publishType = pt
		}
	}

	s.streamName = streamName
	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Error("publish: invalid stream path", "appName", s.appName, "streamName", streamName)
		return
	}

	slog.Info("publish request", "fullStreamPath", fullStreamPath, "publishType", publishType, "transactionID", transactionID)

	// 세션을 발행자 모드로 설정
	s.isPublishing = true
	s.isActive = true

	// 스트림 생성 및 설정
	stream := media.NewStream(fullStreamPath)
	s.SetStream(stream)

	// MediaServer에 publish 시작 알림
	select {
	case s.mediaServerChannel <- media.PublishStarted{
		BaseNodeEvent: media.BaseNodeEvent{
			ID:       s.ID(),
			NodeType: media.MediaNodeTypeRTMP,
		},
		Stream: stream,
	}:
	default:
		slog.Warn("MediaServer channel full, dropping PublishStarted event")
	}

	// onStatus 이벤트 전송: NetStream.Publish.Start
	statusObj := map[string]any{
		"level":       "status",
		"code":        "NetStream.Publish.Start",
		"description": fmt.Sprintf("Started publishing stream %s", fullStreamPath),
		"details":     fullStreamPath,
	}

	statusSequence, err := amf.EncodeAMF0Sequence("onStatus", 0.0, nil, statusObj)
	if err != nil {
		slog.Error("publish: failed to encode onStatus", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, statusSequence)
	if err != nil {
		slog.Error("publish: failed to write onStatus", "err", err)
		return
	}

	slog.Info("publish started successfully", "fullStreamPath", fullStreamPath, "transactionID", transactionID)
}

// play 명령어 처리 (싱크 모드 활성화)
func (s *session) handlePlay(values []any) {

	if len(values) < 3 {
		slog.Error("play: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
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

	s.streamName = streamName
	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Error("play: invalid stream path", "appName", s.appName, "streamName", streamName)
		return
	}

	slog.Info("play request", "fullStreamPath", fullStreamPath, "transactionID", transactionID)

	// 세션을 플레이어 모드로 설정
	s.isPlaying = true
	s.isActive = true

	// MediaServer에 play 시작 알림
	select {
	case s.mediaServerChannel <- media.PlayStarted{
		BaseNodeEvent: media.BaseNodeEvent{
			ID:       s.ID(),
			NodeType: media.MediaNodeTypeRTMP,
		},
		StreamId: fullStreamPath,
	}:
	default:
		slog.Warn("MediaServer channel full, dropping PlayStarted event")
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

	err = s.writer.writeCommand(s.conn, resetSequence)
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

	err = s.writer.writeCommand(s.conn, startSequence)
	if err != nil {
		slog.Error("play: failed to write start onStatus", "err", err)
		return
	}

	slog.Info("play started successfully", "fullStreamPath", fullStreamPath, "transactionID", transactionID)
}

// 오디오 데이터 처리 (발행자 모드에서만)
func (s *session) handleAudio(message *Message) {
	if !s.IsPublisher() {
		slog.Warn("received audio data but not publishing")
		return
	}

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Warn("received audio data but no valid stream path")
		return
	}

	// Zero-copy: payload를 그대로 사용
	if len(message.payload) == 0 {
		slog.Warn("empty audio data received")
		return
	}

	// 첫 번째 청크의 첫 번째 바이트로 오디오 정보 추출
	firstByte := message.payload[0][0]
	frameType := s.parseAudioFrameType(firstByte, message.payload)

	// 스트림에 직접 오디오 프레임 전송 (ManagedFrame 사용)
	if s.stream != nil {
		// Pool 추적이 가능한 ManagedFrame 생성
		managedFrame := convertRTMPFrameToManagedFrame(frameType, message.messageHeader.timestamp, message.payload, false, s.reader.readerContext.poolManager)

		// ManagedFrame을 직접 전송 (zero-copy 최적화)
		s.stream.SendManagedFrame(managedFrame)
		managedFrame.Release() // Stream에서 처리 후 pool 반납
	} else {
		slog.Warn("No stream connected for audio data", "sessionId", s.sessionId)
	}
}

// 비디오 데이터 처리 (발행자 모드에서만)
func (s *session) handleVideo(message *Message) {
	if !s.IsPublisher() {
		slog.Warn("received video data but not publishing")
		return
	}

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Warn("received video data but no valid stream path")
		return
	}

	// Zero-copy: payload를 그대로 사용
	if len(message.payload) == 0 {
		slog.Warn("empty video data received")
		return
	}

	// 첫 번째 청크의 첫 번째 바이트로 비디오 정보 추출
	firstByte := message.payload[0][0]
	frameType := s.parseVideoFrameType(firstByte, message.payload)

	// 스트림에 직접 비디오 프레임 전송 (ManagedFrame 사용)
	if s.stream != nil {
		// Pool 추적이 가능한 ManagedFrame 생성
		managedFrame := convertRTMPFrameToManagedFrame(frameType, message.messageHeader.timestamp, message.payload, true, s.reader.readerContext.poolManager)

		// ManagedFrame을 직접 전송 (zero-copy 최적화)
		s.stream.SendManagedFrame(managedFrame)
		managedFrame.Release() // Stream에서 처리 후 pool 반납
	} else {
		slog.Warn("No stream connected for video data", "sessionId", s.sessionId)
	}
}

// parseAudioFrameType 오디오 프레임 타입 파싱
func (s *session) parseAudioFrameType(firstByte byte, payload [][]byte) RTMPFrameType {
	// AAC 특수 처리
	if ((firstByte>>4)&0x0F) == 10 && len(payload[0]) > 1 {
		switch payload[0][1] {
		case 0:
			return RTMPFrameTypeAACSequenceHeader
		case 1:
			return RTMPFrameTypeAACRaw
		}
	}

	// 기본적으로는 raw 오디오로 처리
	return RTMPFrameTypeAACRaw
}

// parseVideoFrameType 비디오 프레임 타입 파싱
func (s *session) parseVideoFrameType(firstByte byte, payload [][]byte) RTMPFrameType {
	// 프레임 타입 (4비트)
	frameTypeFlag := (firstByte >> 4) & 0x0F
	codecId := firstByte & 0x0F

	// H.264 특수 처리
	if codecId == 7 && len(payload[0]) > 1 {
		avcPacketType := payload[0][1]
		switch avcPacketType {
		case 0:
			return RTMPFrameTypeAVCSequenceHeader
		case 1:
			if frameTypeFlag == 1 {
				return RTMPFrameTypeKeyFrame
			}
			return RTMPFrameTypeInterFrame
		case 2:
			return RTMPFrameTypeAVCEndOfSequence
		}
	}

	// 일반적인 프레임 타입
	switch frameTypeFlag {
	case 1:
		return RTMPFrameTypeKeyFrame
	case 2:
		return RTMPFrameTypeInterFrame
	case 3:
		return RTMPFrameTypeDisposableInterFrame
	case 4:
		return RTMPFrameTypeGeneratedKeyFrame
	case 5:
		return RTMPFrameTypeVideoInfoFrame
	default:
		return RTMPFrameTypeInterFrame
	}
}

// handleScriptData 스크립트 데이터 처리 (메타데이터 등)
func (s *session) handleScriptData(message *Message) {

	// AMF 데이터 디코딩
	reader := ConcatByteSlicesReader(message.payload)
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
	case "onTextData":
		s.handleOnTextData(values)
	default:
		slog.Info("unknown script command", "command", commandName, "values", values)
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

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Warn("received metadata but no valid stream path")
		return
	}

	// 두 번째 값은 메타데이터 객체
	metadata, ok := values[1].(map[string]any)
	if !ok {
		slog.Error("onMetaData: invalid metadata object", "type", utils.TypeName(values[1]))
		return
	}

	// 스트림에 직접 메타데이터 전송
	if s.stream != nil {
		// any 값을 string으로 변환
		stringMetadata := make(map[string]string)
		for k, v := range metadata {
			stringMetadata[k] = fmt.Sprintf("%v", v)
		}

		s.stream.SendMetadata(stringMetadata)
		slog.Info("Metadata sent to stream", "sessionId", s.sessionId, "metadataKeys", len(metadata))
	} else {
		slog.Warn("No stream connected for metadata", "sessionId", s.sessionId)
	}
}

// handleOnTextData 텍스트 데이터 처리
func (s *session) handleOnTextData(values []any) {
	// TODO: 텍스트 데이터 처리
}

// appname/streamkey 조합의 전체 스트림 경로를 반환
func (s *session) GetFullStreamPath() string {
	if s.appName == "" || s.streamName == "" {
		return ""
	}
	return s.appName + "/" + s.streamName
}

// 세션 정보를 반환
func (s *session) GetStreamInfo() (streamID uint32, streamName string, isPublishing bool, isPlaying bool) {
	return s.streamID, s.streamName, s.isPublishing, s.isPlaying
}

// 세션 정리
func (s *session) cleanup() {
	fullStreamPath := s.GetFullStreamPath()

	// 예기치 않은 종료 시 적절한 이벤트 전송
	if s.isPublishing {
		s.stopPublishing()
	}
	if s.isPlaying {
		s.stopPlaying()
	}

	s.isActive = false
	s.stream = nil
	s.streamID = 0
	s.streamName = ""
	s.appName = ""

	slog.Info("session cleanup completed", "sessionId", s.sessionId, "fullStreamPath", fullStreamPath)
}

// 구현을 위한 헬퍼 메서드들

// sendVideoFrame 비디오 프레임을 RTMP 메시지로 전송
func (s *session) sendVideoFrame(frame media.Frame) error {
	message := &Message{
		messageHeader: &messageHeader{
			timestamp: frame.Timestamp,
			length:    uint32(s.calculateDataSize(frame.Data)),
			typeId:    MsgTypeVideo,
			streamId:  s.streamID,
		},
		payload: frame.Data,
	}

	return s.writer.writeVideoMessage(s.conn, message)
}

// sendAudioFrame 오디오 프레임을 RTMP 메시지로 전송
func (s *session) sendAudioFrame(frame media.Frame) error {
	message := &Message{
		messageHeader: &messageHeader{
			timestamp: frame.Timestamp,
			length:    uint32(s.calculateDataSize(frame.Data)),
			typeId:    MsgTypeAudio,
			streamId:  s.streamID,
		},
		payload: frame.Data,
	}

	return s.writer.writeAudioMessage(s.conn, message)
}

// sendMetadataToClient 메타데이터를 RTMP onMetaData 메시지로 전송
func (s *session) sendMetadataToClient(metadata map[string]string) error {
	// string을 interface{} map으로 변환 (AMF 인코딩을 위해)
	interfaceMetadata := make(map[string]interface{})
	for k, v := range metadata {
		interfaceMetadata[k] = v
	}

	// AMF0 onMetaData 시퀀스로 인코딩
	values := []interface{}{"onMetaData", interfaceMetadata}

	// AMF0 시퀀스를 바이트로 인코딩
	encodedData, err := amf.EncodeAMF0Sequence(values...)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	// script data 메시지로 전송
	payload := [][]byte{encodedData}
	message := &Message{
		messageHeader: &messageHeader{
			timestamp: 0,
			length:    uint32(len(encodedData)),
			typeId:    MsgTypeAMF0Data,
			streamId:  s.streamID,
		},
		payload: payload,
	}

	return s.writer.writeScriptMessage(s.conn, message)
}

// calculateDataSize 데이터 크기 계산 헬퍼 함수
func (s *session) calculateDataSize(data [][]byte) int {
	totalSize := 0
	for _, chunk := range data {
		totalSize += len(chunk)
	}
	return totalSize
}

// 바이트 슬라이스들을 Reader로 변환
func ConcatByteSlicesReader(slices [][]byte) io.Reader {
	readers := make([]io.Reader, 0, len(slices))
	for _, b := range slices {
		readers = append(readers, bytes.NewReader(b))
	}
	return io.MultiReader(readers...)
}

// newSession 새로운 세션 생성 (내부 사용)
func newSession(conn net.Conn, mediaServerChannel chan<- any) *session {
	s := &session{
		reader:             newMessageReader(),
		writer:             newMessageWriter(),
		conn:               conn,
		mediaServerChannel: mediaServerChannel,
		isActive:           true,
	}

	// 포인터 주소값을 sessionId로 사용
	s.sessionId = fmt.Sprintf("%p", s)

	// NodeCreated 이벤트 전송
	select {
	case s.mediaServerChannel <- media.NodeCreated{
		BaseNodeEvent: media.BaseNodeEvent{
			ID:       s.ID(),
			NodeType: media.MediaNodeTypeRTMP,
		},
		Node: s,
	}:
	default:
		slog.Warn("MediaServer channel full, dropping NodeCreated event")
	}

	go s.handleRead()

	return s
}

// handleRead 읽기 처리
func (s *session) handleRead() {
	defer func() {
		s.cleanup()
		if s.conn != nil {
			if err := s.conn.Close(); err != nil {
				slog.Error("Error closing connection", "sessionId", s.sessionId, "err", err)
			}
		}

		// MediaServer에 session 종료 알림
		select {
		case s.mediaServerChannel <- media.NodeTerminated{
			BaseNodeEvent: media.BaseNodeEvent{
				ID:       s.ID(),
				NodeType: media.MediaNodeTypeRTMP,
			},
		}:
		default:
			slog.Warn("MediaServer channel full, dropping NodeTerminated event")
		}
	}()

	if err := handshake(s.conn); err != nil {
		slog.Error("Handshake failed", "err", err)
		return
	}

	slog.Info("Handshake successful with", "addr", s.conn.RemoteAddr())

	for {
		message, err := s.reader.readNextMessage(s.conn)
		if err != nil {
			if err != io.EOF {
				slog.Error("Error reading message", "sessionId", s.sessionId, "err", err)
			}
			return
		}

		switch message.messageHeader.typeId {
		case MsgTypeSetChunkSize:
			s.handleSetChunkSize(message)
		default:
			s.handleMessage(message)
		}
	}
}

// handleMessage 메시지 처리
func (s *session) handleMessage(message *Message) {
	slog.Debug("receive message", "sessionId", s.sessionId, "typeId", message.messageHeader.typeId)
	switch message.messageHeader.typeId {
	case MsgTypeSetChunkSize:
		s.handleSetChunkSize(message)
	case MsgTypeAbort:
		// Optional: ignore or log
	case MsgTypeAcknowledgement:
		// 서버용: 클라이언트의 ack 수신
	case MsgTypeUserControl:
		//s.handleUserControl(message)
	case MsgTypeWindowAckSize:
		// 클라이언트가 설정한 ack 윈도우 크기
	case MsgTypeSetPeerBW:
		// bandwidth 제한에 대한 정보
	case MsgTypeAudio:
		s.handleAudio(message)
	case MsgTypeVideo:
		s.handleVideo(message)
	case MsgTypeAMF3Data:
		// AMF3 포맷. 대부분 Flash Player
	case MsgTypeAMF3SharedObject:
	case MsgTypeAMF3Command:
	case MsgTypeAMF0Data:
		s.handleScriptData(message)
	case MsgTypeAMF0Command:
		s.handleAMF0Command(message)
	default:
		slog.Warn("unhandled RTMP message type", "sessionId", s.sessionId, "type", message.messageHeader.typeId)
	}
}

// handleSetChunkSize 청크 크기 설정 처리
func (s *session) handleSetChunkSize(message *Message) {
	slog.Debug("handleSetChunkSize", "sessionId", s.sessionId)

	// 전체 payload 길이 계산
	totalLength := 0
	for _, chunk := range message.payload {
		totalLength += len(chunk)
	}

	if totalLength != 4 {
		slog.Error("Invalid Set Chunk Size message length", "length", totalLength, "sessionId", s.sessionId)
		return
	}

	// 첫 번째 청크에서 4바이트 읽기 (big endian)
	var chunkSizeBytes []byte
	if len(message.payload) > 0 && len(message.payload[0]) >= 4 {
		chunkSizeBytes = message.payload[0][:4]
	} else {
		// 여러 청크에 걸쳐있는 경우 합치기
		chunkSizeBytes = make([]byte, 0, 4)
		for _, chunk := range message.payload {
			chunkSizeBytes = append(chunkSizeBytes, chunk...)
			if len(chunkSizeBytes) >= 4 {
				chunkSizeBytes = chunkSizeBytes[:4]
				break
			}
		}
	}

	newChunkSize := binary.BigEndian.Uint32(chunkSizeBytes)

	// 첫 번째 비트(최상위 비트) 체크: 반드시 0이어야 함
	if newChunkSize&0x80000000 != 0 {
		slog.Error("Set Chunk Size has reserved highest bit set", "value", newChunkSize, "sessionId", s.sessionId)
		return
	}

	// RTMP 최대 청크 크기 제한 (1 ~ 16777215)
	if newChunkSize < 1 || newChunkSize > ExtendedTimestampThreshold {
		slog.Error("Set Chunk Size out of valid range", "value", newChunkSize, "sessionId", s.sessionId)
		return
	}

	// 실제 세션 청크 크기 적용
	s.reader.setChunkSize(newChunkSize)
}

// handleAMF0Command AMF0 명령어 처리
func (s *session) handleAMF0Command(message *Message) {
	slog.Debug("handleAMF0Command", "sessionId", s.sessionId)
	reader := ConcatByteSlicesReader(message.payload)
	values, err := amf.DecodeAMF0Sequence(reader)
	if err != nil {
		slog.Error("Failed to decode AMF0 sequence", "sessionId", s.sessionId, "err", err)
		return
	}

	if len(values) == 0 {
		slog.Error("Empty AMF0 sequence", "sessionId", s.sessionId)
		return
	}

	commandName, ok := values[0].(string)
	if !ok {
		slog.Error("Invalid command name type", "sessionId", s.sessionId, "actual", utils.TypeName(values[0]))
		return
	}

	switch commandName {
	case "connect":
		s.handleConnect(values)
	case "createStream":
		s.handleCreateStream(values)
	case "publish":
		s.handlePublish(values)
	case "play":
		s.handlePlay(values)
	case "pause":
		s.handlePause(values)
	case "deleteStream":
		s.handleDeleteStream(values)
	case "closeStream":
		s.handleCloseStream(values)
	case "releaseStream":
		s.handleReleaseStream(values)
	case "FCPublish":
		s.handleFCPublish(values)
	case "FCUnpublish":
		s.handleFCUnpublish(values)
	case "receiveAudio":
		s.handleReceiveAudio(values)
	case "receiveVideo":
		s.handleReceiveVideo(values)
	case "onBWDone":
		s.handleOnBWDone(values)
	default:
		slog.Error("Unknown AMF0 command", "sessionId", s.sessionId, "name", commandName)
	}
}

// 기타 핸들러들 (기존 RTMP에서 가져옴)
func (s *session) handlePause(values []any) {
	// TODO: 구현
}

func (s *session) handleDeleteStream(values []any) {

	// 발행 중이었다면 발행 중단 이벤트 전송
	if s.isPublishing {
		s.stopPublishing()
	}

	// 재생 중이었다면 재생 중단 이벤트 전송
	if s.isPlaying {
		s.stopPlaying()
	}

	// 스트림 상태 초기화
	s.streamID = 0
	s.streamName = ""
	s.stream = nil
}

func (s *session) handleCloseStream(values []any) {

	// deleteStream과 동일한 처리
	if s.isPublishing {
		s.stopPublishing()
	}

	if s.isPlaying {
		s.stopPlaying()
	}

	// 스트림 상태 초기화
	s.streamID = 0
	s.streamName = ""
	s.stream = nil
}

func (s *session) handleReleaseStream(values []any) {
	// TODO: 구현
}

func (s *session) handleFCPublish(values []any) {
	// TODO: 구현
}

func (s *session) handleFCUnpublish(values []any) {

	// 발행 중단 처리
	if s.isPublishing {
		s.stopPublishing()
	}
}

func (s *session) handleReceiveAudio(values []any) {
	// TODO: 구현
}

func (s *session) handleReceiveVideo(values []any) {
	// TODO: 구현
}

func (s *session) handleOnBWDone(values []any) {
	// TODO: 구현
}

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
			slog.Info("app name extracted", "appName", appName)
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

	err = s.writer.writeSetChunkSize(s.conn, 4096)
	if err != nil {
		return
	}

	// 서버 측에서도 청크 크기 설정 (들어오는 데이터 처리용)
	s.reader.setChunkSize(4096)

	err = s.writer.writeCommand(s.conn, sequence)
	if err != nil {
		return
	}
}

// stopPublishing 발행 중단 처리
func (s *session) stopPublishing() {
	if !s.isPublishing {
		return
	}

	s.isPublishing = false

	// MediaServer에 발행 중단 이벤트 전송
	select {
	case s.mediaServerChannel <- media.PublishStopped{
		BaseNodeEvent: media.BaseNodeEvent{
			ID:       s.ID(),
			NodeType: media.MediaNodeTypeRTMP,
		},
		StreamId: s.streamName,
	}:
		slog.Info("PublishStopped event sent", "sessionId", s.sessionId, "streamName", s.streamName)
	default:
		slog.Warn("MediaServer channel full, dropping PublishStopped event", "sessionId", s.sessionId)
	}
}

// stopPlaying 재생 중단 처리
func (s *session) stopPlaying() {
	if !s.isPlaying {
		return
	}

	s.isPlaying = false

	// MediaServer에 재생 중단 이벤트 전송
	select {
	case s.mediaServerChannel <- media.PlayStopped{
		BaseNodeEvent: media.BaseNodeEvent{
			ID:       s.ID(),
			NodeType: media.MediaNodeTypeRTMP,
		},
		StreamId: s.streamName,
	}:
		slog.Info("PlayStopped event sent", "sessionId", s.sessionId, "streamName", s.streamName)
	default:
		slog.Warn("MediaServer channel full, dropping PlayStopped event", "sessionId", s.sessionId)
	}
}
