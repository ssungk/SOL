package rtmp2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/amf"
)

// session RTMP2에서 사용하는 세션 구조체 (소스/싱크 패턴 기반)
type session struct {
	reader          *messageReader
	writer          *messageWriter
	conn            net.Conn
	externalChannel chan<- interface{}
	messageChannel  chan *Message

	// 세션 식별자 - 포인터 주소값 기반
	sessionId string

	// 스트림 관리
	streamID   uint32
	streamName string // streamkey
	appName    string // appname

	// 소스/싱크 관리
	rtmpSource *RTMPSource // 발행자일 때
	rtmpSink   *RTMPSink   // 플레이어일 때

	// 상태 관리
	isPublishing bool
	isPlaying    bool
}

// GetID 세션의 ID를 반환 (sessionId 필드)
func (s *session) GetID() string {
	return s.sessionId
}

// SetRTMPSource RTMP 소스 설정 (발행자 모드)
func (s *session) SetRTMPSource(source *RTMPSource) {
	s.rtmpSource = source
	s.isPublishing = true
	
	slog.Info("RTMP source set for session", 
		"sessionId", s.sessionId,
		"sourceId", source.GetSourceId())
}

// SetRTMPSink RTMP 싱크 설정 (플레이어 모드)
func (s *session) SetRTMPSink(sink *RTMPSink) {
	s.rtmpSink = sink
	s.isPlaying = true
	
	// 메시지 writer를 싱크에 연결
	sink.SetWriter(s.writer)
	
	slog.Info("RTMP sink set for session", 
		"sessionId", s.sessionId,
		"sinkId", sink.GetSinkId())
}

// GetRTMPSource RTMP 소스 반환
func (s *session) GetRTMPSource() *RTMPSource {
	return s.rtmpSource
}

// GetRTMPSink RTMP 싱크 반환
func (s *session) GetRTMPSink() *RTMPSink {
	return s.rtmpSink
}

// IsPublisher 발행자 여부 확인
func (s *session) IsPublisher() bool {
	return s.isPublishing && s.rtmpSource != nil
}

// IsPlayer 플레이어 여부 확인
func (s *session) IsPlayer() bool {
	return s.isPlaying && s.rtmpSink != nil
}

// createStream 명령어 처리
func (s *session) handleCreateStream(values []any) {
	slog.Info("handling createStream", "params", values)

	if len(values) < 2 {
		slog.Error("createStream: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("createStream: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
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
	slog.Info("handling publish", "params", values)

	if len(values) < 3 {
		slog.Error("publish: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("publish: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	// 스트림 이름
	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("publish: invalid stream name", "type", fmt.Sprintf("%T", values[3]))
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

	// RTMP 소스 생성
	sessionInfo := RTMPSessionInfo{
		SessionID:  s.sessionId,
		StreamName: streamName,
		AppName:    s.appName,
		StreamID:   s.streamID,
		Type:       RTMPSessionTypePublisher,
	}
	
	rtmpSource := NewRTMPSource(sessionInfo, s.conn.RemoteAddr().String())
	s.SetRTMPSource(rtmpSource)

	// Publish 시작 이벤트 전송
	s.sendEvent(PublishStarted{
		SessionId:  s.sessionId,
		StreamName: fullStreamPath,
		StreamId:   s.streamID,
	})

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
	slog.Info("handling play", "params", values)

	if len(values) < 3 {
		slog.Error("play: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("play: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	// 스트림 이름
	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("play: invalid stream name", "type", fmt.Sprintf("%T", values[3]))
		return
	}

	s.streamName = streamName
	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Error("play: invalid stream path", "appName", s.appName, "streamName", streamName)
		return
	}

	slog.Info("play request", "fullStreamPath", fullStreamPath, "transactionID", transactionID)

	// RTMP 싱크 생성
	sessionInfo := RTMPSessionInfo{
		SessionID:  s.sessionId,
		StreamName: streamName,
		AppName:    s.appName,
		StreamID:   s.streamID,
		Type:       RTMPSessionTypePlayer,
	}
	
	rtmpSink := NewRTMPSink(sessionInfo, s.conn, s.conn.RemoteAddr().String())
	s.SetRTMPSink(rtmpSink)

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

	// 3. Play 시작 이벤트 전송
	s.sendEvent(PlayStarted{
		SessionId:  s.sessionId,
		StreamName: fullStreamPath,
		StreamId:   s.streamID,
	})

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
	
	// RTMP 소스를 통해 오디오 프레임 전송
	if err := s.rtmpSource.SendAudioFrame(frameType, message.messageHeader.Timestamp, message.payload); err != nil {
		slog.Error("Failed to send audio frame through RTMP source", 
			"sessionId", s.sessionId,
			"err", err)
		return
	}

	slog.Debug("Audio frame processed through RTMP source", 
		"sessionId", s.sessionId,
		"frameType", string(frameType),
		"timestamp", message.messageHeader.Timestamp)
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
	
	// RTMP 소스를 통해 비디오 프레임 전송
	if err := s.rtmpSource.SendVideoFrame(frameType, message.messageHeader.Timestamp, message.payload); err != nil {
		slog.Error("Failed to send video frame through RTMP source", 
			"sessionId", s.sessionId,
			"err", err)
		return
	}

	slog.Debug("Video frame processed through RTMP source", 
		"sessionId", s.sessionId,
		"frameType", string(frameType),
		"timestamp", message.messageHeader.Timestamp)
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
	slog.Info("received script data")

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
		slog.Error("invalid script command name", "type", fmt.Sprintf("%T", values[0]))
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
	slog.Info("received onMetaData")

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
		slog.Error("onMetaData: invalid metadata object", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	// RTMP 소스를 통해 메타데이터 전송
	if err := s.rtmpSource.SendMetadata(metadata); err != nil {
		slog.Error("Failed to send metadata through RTMP source", 
			"sessionId", s.sessionId,
			"err", err)
		return
	}

	slog.Info("Metadata processed through RTMP source", 
		"sessionId", s.sessionId,
		"metadataKeys", len(metadata))
}

// handleOnTextData 텍스트 데이터 처리
func (s *session) handleOnTextData(values []any) {
	slog.Info("received onTextData", "values", values)
	// TODO: 텍스트 데이터 처리
}

// GetFullStreamPath appname/streamkey 조합의 전체 스트림 경로를 반환
func (s *session) GetFullStreamPath() string {
	if s.appName == "" || s.streamName == "" {
		return ""
	}
	return s.appName + "/" + s.streamName
}

// GetStreamInfo 세션 정보를 반환
func (s *session) GetStreamInfo() (streamID uint32, streamName string, isPublishing bool, isPlaying bool) {
	return s.streamID, s.streamName, s.isPublishing, s.isPlaying
}

// 세션 정리
func (s *session) cleanup() {
	fullStreamPath := s.GetFullStreamPath()
	
	// RTMP 소스 정리
	if s.rtmpSource != nil {
		s.rtmpSource.Stop()
		// Publish 종료 이벤트 전송
		s.sendEvent(PublishStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
		s.rtmpSource = nil
	}
	
	// RTMP 싱크 정리
	if s.rtmpSink != nil {
		s.rtmpSink.Stop()
		// Play 종료 이벤트 전송
		s.sendEvent(PlayStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
		s.rtmpSink = nil
	}

	s.isPublishing = false
	s.isPlaying = false
	s.streamID = 0
	s.streamName = ""
	s.appName = ""

	slog.Info("session cleanup completed", "sessionId", s.sessionId, "fullStreamPath", fullStreamPath)
}

// 이벤트 전송 헬퍼 메서드
func (s *session) sendEvent(event interface{}) {
	select {
	case s.externalChannel <- event:
		// 이벤트 전송 성공
	default:
		// 채널이 꽉 찬 경우 이벤트 드롭
		slog.Warn("event channel full, dropping event", "sessionId", s.sessionId, "eventType", fmt.Sprintf("%T", event))
	}
}

// ConcatByteSlicesReader 바이트 슬라이스들을 Reader로 변환
func ConcatByteSlicesReader(slices [][]byte) io.Reader {
	readers := make([]io.Reader, 0, len(slices))
	for _, b := range slices {
		readers = append(readers, bytes.NewReader(b))
	}
	return io.MultiReader(readers...)
}

// newSession 새로운 세션 생성 (내부 사용)
func newSession(conn net.Conn) *session {
	s := &session{
		reader:          newMessageReader(),
		writer:          newMessageWriter(),
		conn:            conn,
		externalChannel: make(chan interface{}, 10),
		messageChannel:  make(chan *Message, 10),
	}

	// 포인터 주소값을 sessionId로 사용
	s.sessionId = fmt.Sprintf("%p", s)

	go s.handleRead()
	go s.handleEvent()

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
		// 종료 이벤트 전송
		s.sendEvent(Terminated{Id: s.sessionId})
	}()

	if err := handshake(s.conn); err != nil {
		slog.Info("Handshake failed:", "err", err)
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
		case MSG_TYPE_SET_CHUNK_SIZE:
			s.handleSetChunkSize(message)
		default:
			s.handleMessage(message)
		}
	}
}

// handleEvent 이벤트 처리
func (s *session) handleEvent() {
	for {
		select {
		case message := <-s.messageChannel:
			s.handleMessage(message)
		}
	}
}

// handleMessage 메시지 처리
func (s *session) handleMessage(message *Message) {
	slog.Debug("receive message", "sessionId", s.sessionId, "typeId", message.messageHeader.typeId)
	switch message.messageHeader.typeId {
	case MSG_TYPE_SET_CHUNK_SIZE:
		s.handleSetChunkSize(message)
	case MSG_TYPE_ABORT:
		// Optional: ignore or log
	case MSG_TYPE_ACKNOWLEDGEMENT:
		// 서버용: 클라이언트의 ack 수신
	case MSG_TYPE_USER_CONTROL:
		//s.handleUserControl(message)
	case MSG_TYPE_WINDOW_ACK_SIZE:
		// 클라이언트가 설정한 ack 윈도우 크기
	case MSG_TYPE_SET_PEER_BW:
		// bandwidth 제한에 대한 정보
	case MSG_TYPE_AUDIO:
		s.handleAudio(message)
	case MSG_TYPE_VIDEO:
		s.handleVideo(message)
	case MSG_TYPE_AMF3_DATA:
		// AMF3 포맷. 대부분 Flash Player
	case MSG_TYPE_AMF3_SHARED_OBJECT:
	case MSG_TYPE_AMF3_COMMAND:
	case MSG_TYPE_AMF0_DATA:
		s.handleScriptData(message)
	case MSG_TYPE_AMF0_COMMAND:
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
	if newChunkSize < 1 || newChunkSize > EXTENDED_TIMESTAMP_THRESHOLD {
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
		slog.Error("Invalid command name type", "sessionId", s.sessionId, "actual", fmt.Sprintf("%T", values[0]))
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
	slog.Info("handling pause", "params", values)
	// TODO: 구현
}

func (s *session) handleDeleteStream(values []any) {
	slog.Info("handling deleteStream", "params", values)
	// TODO: 구현
}

func (s *session) handleCloseStream(values []any) {
	slog.Info("handling closeStream", "params", values)
	// TODO: 구현
}

func (s *session) handleReleaseStream(values []any) {
	slog.Info("handling releaseStream", "params", values)
	// TODO: 구현
}

func (s *session) handleFCPublish(values []any) {
	slog.Info("handling FCPublish", "params", values)
	// TODO: 구현
}

func (s *session) handleFCUnpublish(values []any) {
	slog.Info("handling FCUnpublish", "params", values)
	// TODO: 구현
}

func (s *session) handleReceiveAudio(values []any) {
	slog.Info("handling receiveAudio", "params", values)
	// TODO: 구현
}

func (s *session) handleReceiveVideo(values []any) {
	slog.Info("handling receiveVideo", "params", values)
	// TODO: 구현
}

func (s *session) handleOnBWDone(values []any) {
	slog.Info("handling onBWDone", "params", values)
	// TODO: 구현
}

// handleConnect connect 명령어 처리
func (s *session) handleConnect(values []any) {
	slog.Info("handling connect", "params", values)

	// 최소 3개 요소: "connect", transaction ID, command object
	if len(values) < 3 {
		slog.Error("connect: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("connect: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	slog.Info("handling connect", "transactionID", transactionID)

	// command object (map)
	commandObj, ok := values[2].(map[string]any)
	if !ok {
		slog.Error("connect: invalid command object", "type", fmt.Sprintf("%T", values[2]))
		return
	}

	slog.Info("object", "commandObj", commandObj)

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

	slog.Info("encoded _result sequence", "sequence", sequence)
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

// 기타 필요한 핸들러들 (기존 RTMP 세션에서 가져옴)
// handleReleaseStream, handleFCPublish, handleFCUnpublish, handleCloseStream, handleDeleteStream 등은
// 필요에 따라 기존 코드에서 복사하여 추가