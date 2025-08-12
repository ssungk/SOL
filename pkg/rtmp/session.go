package rtmp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/media"
	"sol/pkg/rtmp/amf"
	"sol/pkg/utils"
	"sync"
	"time"
	"unsafe"
)

// session RTMP에서 사용하는 세션 구조체 (이벤트 루프 기반)
type session struct {
	reader *messageReader
	writer *messageWriter
	conn   net.Conn

	// 스트림 관리 (이벤트 루프 내에서만 접근)
	appName string

	// 발행용 (MediaSource) - RTMP streamId 기반
	publishedStreams map[int]*media.Stream // streamId(int) -> Stream

	// 상호 참조 테이블
	streamIdToPath map[int]string // streamId -> streamPath
	streamPathToId map[string]int // streamPath -> streamId
	nextStreamId   int            // 다음 할당할 streamId (1부터 시작)

	// 재생용 (MediaSink)
	subscribedStreamIds []string // play 시 구독하는 스트림 ID들

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
	s := &session{
		reader:              newMessageReader(),
		writer:              newMessageWriter(),
		conn:                conn,
		mediaServerChannel:  mediaServerChannel,
		channel:             make(chan any, media.DefaultChannelBufferSize),
		ctx:                 ctx,
		cancel:              cancel,
		wg:                  wg,
		publishedStreams:    make(map[int]*media.Stream), // 발행용 스트림 맵 초기화
		streamIdToPath:      make(map[int]string),        // streamId -> streamPath 매핑
		streamPathToId:      make(map[string]int),        // streamPath -> streamId 매핑
		nextStreamId:        1,                           // 1부터 시작
		subscribedStreamIds: make([]string, 0),           // 재생용 스트림 ID 초기화
	}

	// NodeCreated 이벤트를 MediaServer로 전송
	s.mediaServerChannel <- media.NewNodeCreated(s.ID(), media.NodeTypeRTMP, s)

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
			s.channelHandler(data)
		case <-s.ctx.Done():
			return
		}
	}
}

// readLoop 클라이언트로부터 들어오는 RTMP 메시지를 읽는 루프
func (s *session) readLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	// readLoop가 종료되면 (연결 끊김 등) 전체 세션을 종료하도록 cancel 호출
	defer s.cancel()

	if err := ServerHandshake(s.conn); err != nil {
		slog.Error("Handshake failed", "err", err)
		return
	}
	slog.Info("Handshake successful with", "addr", s.conn.RemoteAddr())

	for {
		message, err := s.reader.readNextMessage(s.conn)
		if err != nil {
			return // 에러 발생 시 루프 종료
		}

		// SetChunkSize와 Abort 메시지는 즉시 처리 (레이스 컨디션 방지)
		if message.messageHeader.typeId == MsgTypeSetChunkSize {
			s.handleSetChunkSize(message)
			continue // eventCh로 보내지 않음
		}

		if message.messageHeader.typeId == MsgTypeAbort {
			s.handleAbort(message)
			continue // eventCh로 보내지 않음
		}

		// 다른 메시지들은 이벤트로 변환하여 이벤트 루프로 전송
		s.channel <- commandEvent{message: message}
	}
}

// channelHandler 이벤트 종류에 따라 적절한 핸들러 호출
func (s *session) channelHandler(data any) {
	switch e := data.(type) {
	case sendFrameEvent:
		s.handleSendFrame(e)
	case sendMetadataEvent:
		s.handleSendMetadata(e)
	case commandEvent:
		s.handleCommand(e.message)
	default:
		slog.Warn("Unknown session data type", "type", utils.TypeName(data))
	}
}

// cleanup 세션 종료 시 모든 리소스를 정리
func (s *session) cleanup() {
	utils.CloseWithLog(s.conn)

	// 발행자 또는 플레이어였다면, MediaServer에 종료 이벤트를 보냄
	if s.isPublishingMode() {
		s.stopPublishing()
	}
	s.stopPlaying() // 플레이어 상태 체크 없이 항상 호출 (MediaServer에서 중복 처리 방지)

	// MediaServer에 최종 종료 알림 (blocking - 중요한 이벤트)
	s.mediaServerChannel <- media.NewNodeTerminated(s.ID(), media.NodeTypeRTMP)

	slog.Info("session cleanup completed", "sessionId", s.ID())
}

// --- MediaSink 인터페이스 구현 ---

func (s *session) SendMediaFrame(streamId string, frame media.Frame) error {
	// 재생 중이 아니면 거부
	if s.isPublishingMode() {
		return fmt.Errorf("session is in publishing mode, cannot receive frames")
	}

	// 구독 중인 스트림인지 확인
	subscribing := false
	for _, id := range s.subscribedStreamIds {
		if id == streamId {
			subscribing = true
			break
		}
	}

	// 구독 중이 아니면 거부

	if !subscribing {
		return fmt.Errorf("not subscribed to stream %s", streamId)
	}

	// 이벤트로 전송
	event := sendFrameEvent{frame: frame}
	select {
	case s.channel <- event:
		return nil
	case <-s.ctx.Done():
		return fmt.Errorf("session closed, cannot send frame: %w", s.ctx.Err())
	}
}

func (s *session) SendMetadata(streamId string, metadata map[string]string) error {
	// 재생 중이 아니면 무시
	if s.isPublishingMode() {
		return nil
	}

	// 구독 중인 스트림인지 확인
	subscribing := false
	for _, id := range s.subscribedStreamIds {
		if id == streamId {
			subscribing = true
			break
		}
	}

	// 구독 중이 아니면 거부

	if !subscribing {
		return nil // 구독하지 않은 스트림은 조용히 무시
	}

	// 이벤트로 전송
	event := sendMetadataEvent{metadata: metadata}
	select {
	case s.channel <- event:
		return nil
	case <-s.ctx.Done():
		return fmt.Errorf("session closed, cannot send metadata: %w", s.ctx.Err())
	}
}

// --- 이벤트 핸들러 ---

// handleSendFrame MediaSink로부터 받은 프레임을 클라이언트로 전송
func (s *session) handleSendFrame(e sendFrameEvent) {

	var err error
	switch e.frame.Type {
	case media.TypeVideo:
		err = s.sendVideoFrame(e.frame)
	case media.TypeAudio:
		err = s.sendAudioFrame(e.frame)
	default:
		slog.Warn("Unknown frame type", "type", e.frame.Type, "sessionId", s.ID())
		return
	}

	if err != nil {
		slog.Error("Failed to send frame to RTMP session", "sessionId", s.ID(), "subType", e.frame.SubType, "err", err)
	}
}

// handleSendMetadata MediaSink로부터 받은 메타데이터를 클라이언트로 전송
func (s *session) handleSendMetadata(e sendMetadataEvent) {

	if err := s.sendMetadataToClient(e.metadata); err != nil {
		slog.Error("Failed to send metadata to RTMP session", "sessionId", s.ID(), "err", err)
	}
}

// handleCommand 클라이언트로부터 받은 RTMP 메시지 처리
func (s *session) handleCommand(message *Message) {
	slog.Debug("receive message", "sessionId", s.ID(), "typeId", message.messageHeader.typeId)
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
	s.cancel()
	slog.Info("RTMP session stopping", "sessionId", s.ID())
	return nil
}

func (s *session) IsPublisher() bool {
	return s.isPublishingMode()
}

func (s *session) IsPlayer() bool {
	// 플레이어 상태는 MediaServer에서 관리되므로 항상 false 반환
	// 실제 플레이어 여부는 MediaServer의 Sink 등록 상태로 판단
	return false
}

// (이하 기존 핸들러 함수들은 대부분 그대로 유지되거나, 상태 변경 부분을 수정)

// handleConnect, handleCreateStream, handlePublish, handlePlay 등은
// s.isPublishing = true 와 같은 직접적인 상태 변경 대신,
// 이벤트 루프 내에서 안전하게 상태를 변경하도록 수정됩니다.
// (생략된 코드는 기존 로직을 이벤트 루프 패러다임에 맞게 조정한 것입니다)
// createStream 명령어 처리

// 오디오 데이터 처리 (발행자 모드에서만)
func (s *session) handleAudio(message *Message) {
	if !s.IsPublisher() {
		slog.Warn("received audio data but not publishing")
		return
	}

	// 발행 중인 스트림이 없으면 경고
	if len(s.publishedStreams) == 0 {
		slog.Warn("received audio data but no published streams")
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

	// 모든 발행된 스트림에 오디오 프레임 전송
	if len(s.publishedStreams) > 0 {
		// Pool 추적이 가능한 ManagedFrame 생성
		managedFrame := convertRTMPFrameToManagedFrame(frameType, message.messageHeader.timestamp, message.payload, false, s.reader.readerContext.poolManager)

		// 모든 발행된 스트림에 전송
		for _, stream := range s.publishedStreams {
			stream.SendManagedFrame(managedFrame)
		}
		managedFrame.Release() // Stream에서 처리 후 pool 반납
	} else {
		slog.Warn("No published streams for audio data", "sessionId", s.ID())
	}
}

// 비디오 데이터 처리 (발행자 모드에서만)
func (s *session) handleVideo(message *Message) {
	if !s.IsPublisher() {
		slog.Warn("received video data but not publishing")
		return
	}

	// 발행 중인 스트림이 없으면 경고
	if len(s.publishedStreams) == 0 {
		slog.Warn("received video data but no published streams")
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

	// 모든 발행된 스트림에 비디오 프레임 전송
	if len(s.publishedStreams) > 0 {
		// Pool 추적이 가능한 ManagedFrame 생성
		managedFrame := convertRTMPFrameToManagedFrame(frameType, message.messageHeader.timestamp, message.payload, true, s.reader.readerContext.poolManager)

		// 모든 발행된 스트림에 전송
		for _, stream := range s.publishedStreams {
			stream.SendManagedFrame(managedFrame)
		}
		managedFrame.Release() // Stream에서 처리 후 pool 반납
	} else {
		slog.Warn("No published streams for video data", "sessionId", s.ID())
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

// handleAMF0ScriptData 스크립트 데이터 처리 (메타데이터 등)
func (s *session) handleAMF0ScriptData(message *Message) {

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
		slog.Info("Metadata sent to all published streams", "sessionId", s.ID(), "streamCount", len(s.publishedStreams), "metadataKeys", len(metadata))
	} else {
		slog.Warn("No published streams for metadata", "sessionId", s.ID())
	}
}

// GetStreamInfo 세션 정보를 반환 (다중 스트림 지원)
func (s *session) GetStreamInfo() (streamCount int, isPublishing bool, isPlaying bool) {
	return len(s.publishedStreams), s.isPublishingMode(), false // isPlaying은 항상 false (MediaServer에서 관리)
}

// 구현을 위한 헬퍼 메서드들

// sendVideoFrame 비디오 프레임을 RTMP 메시지로 전송
func (s *session) sendVideoFrame(frame media.Frame) error {
	message := &Message{
		messageHeader: &messageHeader{
			timestamp: frame.Timestamp,
			length:    uint32(s.calculateDataSize(frame.Data)),
			typeId:    MsgTypeVideo,
			streamId:  1, // 기본 스트림 ID 고정
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
			streamId:  1, // 기본 스트림 ID 고정
		},
		payload: frame.Data,
	}

	return s.writer.writeAudioMessage(s.conn, message)
}

// sendMetadataToClient 메타데이터를 RTMP onMetaData 메시지로 전송
func (s *session) sendMetadataToClient(metadata map[string]string) error {
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
	payload := [][]byte{encodedData}
	message := &Message{
		messageHeader: &messageHeader{
			timestamp: 0,
			length:    uint32(len(encodedData)),
			typeId:    MsgTypeAMF0Data,
			streamId:  1, // 기본 스트림 ID 고정
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

// handleSetChunkSize 청크 크기 설정 처리
func (s *session) handleSetChunkSize(message *Message) {
	slog.Debug("handleSetChunkSize", "sessionId", s.ID())

	// 전체 payload 길이 계산
	totalLength := 0
	for _, chunk := range message.payload {
		totalLength += len(chunk)
	}

	if totalLength != 4 {
		slog.Error("Invalid Set Chunk Size message length", "length", totalLength, "sessionId", s.ID())
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

	// 전체 payload 길이 계산
	totalLength := 0
	for _, chunk := range message.payload {
		totalLength += len(chunk)
	}

	if totalLength != 4 {
		slog.Error("Invalid Abort message length", "length", totalLength, "sessionId", s.ID())
		return
	}

	// 첫 번째 청크에서 4바이트 읽기 (big endian)
	var chunkStreamIdBytes []byte
	if len(message.payload) > 0 && len(message.payload[0]) >= 4 {
		chunkStreamIdBytes = message.payload[0][:4]
	} else {
		// 여러 청크에 걸쳐있는 경우 합치기
		chunkStreamIdBytes = make([]byte, 0, 4)
		for _, chunk := range message.payload {
			chunkStreamIdBytes = append(chunkStreamIdBytes, chunk...)
			if len(chunkStreamIdBytes) >= 4 {
				chunkStreamIdBytes = chunkStreamIdBytes[:4]
				break
			}
		}
	}

	chunkStreamId := binary.BigEndian.Uint32(chunkStreamIdBytes)

	// Reader에서 해당 청크 스트림 상태 초기화
	s.reader.abortChunkStream(chunkStreamId)

	slog.Info("Chunk stream aborted", "chunkStreamId", chunkStreamId, "sessionId", s.ID())
}

// handleAMF0Command AMF0 명령어 처리
func (s *session) handleAMF0Command(message *Message) {
	slog.Debug("handleAMFCommand", "sessionId", s.ID())
	reader := ConcatByteSlicesReader(message.payload)
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
	case "connect":
		s.handleConnect(values)
	case "createStream":
		s.handleCreateStream(values)
	case "releaseStream":
		s.handleReleaseStream(values)
	case "FCPublish":
		s.handleFCPublish(values)
	case "publish":
		s.handlePublish(values)
	case "play":
		s.handlePlay(values)
	case "FCUnpublish":
		s.handleFCUnpublish(values)
	case "closeStream":
		s.handleCloseStream(values)
	case "deleteStream":
		s.handleDeleteStream(values)
	default:
		slog.Warn("Unknown AMF command", "sessionId", s.ID(), "name", commandName)
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

	// 새로운 스트림 ID 생성 (실제 사용할 streamId)
	newStreamID := s.generateStreamId()

	// _result 응답 전송
	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, float64(newStreamID))
	if err != nil {
		slog.Error("createStream: failed to encode response", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, sequence)
	if err != nil {
		slog.Error("createStream: failed to write response", "err", err)
		return
	}

	slog.Info("createStream successful", "streamID", newStreamID, "transactionID", transactionID)
}

// handleReleaseStream Flash Media Server 호환을 위한 releaseStream 명령어 처리
func (s *session) handleReleaseStream(values []any) {
	slog.Debug("releaseStream command received (ignored)", "sessionId", s.ID())
	// Flash Media Server 호환을 위한 명령어, 특별한 처리 불필요
}

// handleFCPublish Flash Media Server 호환을 위한 FCPublish 명령어 처리  
func (s *session) handleFCPublish(values []any) {
	slog.Debug("FCPublish command received (ignored)", "sessionId", s.ID())
	// Flash Media Server 호환을 위한 명령어, 특별한 처리 불필요
}

// handlePublish publish 명령어 처리 (소스 모드 활성화)
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

	fullStreamPath := s.appName + "/" + streamName
	if s.appName == "" || streamName == "" {
		slog.Error("publish: invalid stream path", "appName", s.appName, "streamKey", streamName)
		return
	}

	slog.Info("publish request", "fullStreamPath", fullStreamPath, "publishType", publishType, "transactionID", transactionID)

	// 1단계: streamId 생성 및 스트림 준비
	streamId := s.generateStreamId()
	stream := media.NewStream(fullStreamPath)
	s.addPublishedStream(streamId, fullStreamPath, stream)

	// 2단계: MediaServer에 실제 publish 시도 (collision detection + 원자적 점유)
	if !s.attemptStreamPublish(fullStreamPath, stream) {
		// 실패 시 정리
		s.removePublishedStream(streamId)
		s.sendPublishErrorResponse(transactionID, "NetStream.Publish.BadName", "Stream key was taken by another client")
		return
	}

	// 3단계: 성공 응답 전송
	s.sendPublishSuccessResponse(transactionID, fullStreamPath)

	slog.Info("publish started successfully", "fullStreamPath", fullStreamPath, "transactionID", transactionID)
}

// handlePlay play 명령어 처리 (싱크 모드 활성화)
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

	fullStreamPath := s.appName + "/" + streamName
	if s.appName == "" || streamName == "" {
		slog.Error("play: invalid stream path", "appName", s.appName, "streamKey", streamName)
		return
	}

	slog.Info("play request", "fullStreamPath", fullStreamPath, "transactionID", transactionID)

	// 구독 스트림 ID에 추가
	s.addSubscribedStreamId(fullStreamPath)

	// MediaServer에 play 시작 알림
	select {
	case s.mediaServerChannel <- media.NewPlayStarted(s.ID(), media.NodeTypeRTMP, fullStreamPath):
	case <-s.ctx.Done():
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

// stopPublishing 발행 중단 처리
func (s *session) stopPublishing() {
	if !s.isPublishingMode() {
		return
	}

	// 다중 스트림 발행 중단 처리
	for streamId := range s.publishedStreams {
		if streamPath, exists := s.getStreamPathById(streamId); exists {
			select {
			case s.mediaServerChannel <- media.NewPublishStopped(s.ID(), media.NodeTypeRTMP, streamPath):
			case <-s.ctx.Done():
			}
		}
	}

	// 스트림 정리
	s.publishedStreams = make(map[int]*media.Stream)
	s.streamIdToPath = make(map[int]string)
	s.streamPathToId = make(map[string]int)
}

// stopPlaying 재생 중단 처리
func (s *session) stopPlaying() {
	// 상태 체크 제거: MediaServer에서 중복 이벤트 처리 방지

	// 모든 구독된 스트림에 대해 재생 중단 이벤트 전송
	for _, streamId := range s.subscribedStreamIds {
		select {
		case s.mediaServerChannel <- media.NewPlayStopped(s.ID(), media.NodeTypeRTMP, streamId):
		case <-s.ctx.Done():
		}
	}

	// 구독 스트림 정리
	s.subscribedStreamIds = s.subscribedStreamIds[:0]
}

// handleAMF3ScriptData AMF3 스크립트 데이터 처리
func (s *session) handleAMF3ScriptData(message *Message) {
	// AMF3 데이터 디코딩
	reader := ConcatByteSlicesReader(message.payload)

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

	reader := ConcatByteSlicesReader(message.payload)

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
	case "connect":
		s.handleConnect(values)
	case "createStream":
		s.handleCreateStream(values)
	case "publish":
		s.handlePublish(values)
	case "play":
		s.handlePlay(values)
	case "closeStream":
		s.handleCloseStream(values)
	case "FCUnpublish":
		s.handleFCUnpublish(values)
	case "deleteStream":
		s.handleDeleteStream(values)
	case "releaseStream":
		// Flash Media Server 호환을 위한 명령어, 특별한 처리 불필요
		slog.Debug("releaseStream command received (ignored)", "sessionId", s.ID())
	case "FCPublish":
		// Flash Media Server 호환을 위한 명령어, 특별한 처리 불필요
		slog.Debug("FCPublish command received (ignored)", "sessionId", s.ID())
	default:
		slog.Warn("Unsupported AMF3 command", "sessionId", s.ID(), "command", commandName)
	}
}

// --- 다중 스트림 관리 메서드들 ---

// addPublishedStream 발행 스트림 추가 (streamId 기반)
func (s *session) addPublishedStream(streamId int, streamPath string, stream *media.Stream) {
	s.publishedStreams[streamId] = stream
	s.addStreamMapping(streamId, streamPath)
	slog.Debug("Published stream added", "sessionId", s.ID(), "streamId", streamId, "streamPath", streamPath, "mediaStreamId", stream.ID())
}

// removePublishedStream 발행 스트림 제거 (streamId 기반)
func (s *session) removePublishedStream(streamId int) *media.Stream {
	stream, exists := s.publishedStreams[streamId]
	if exists {
		delete(s.publishedStreams, streamId)
		s.removeStreamMapping(streamId)
		slog.Debug("Published stream removed", "sessionId", s.ID(), "streamId", streamId)
	}
	return stream
}

// getPublishedStream 발행 스트림 가져오기 (streamId 기반)
func (s *session) getPublishedStream(streamId int) (*media.Stream, bool) {
	stream, exists := s.publishedStreams[streamId]
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

// addSubscribedStreamId 구독 스트림 ID 추가
func (s *session) addSubscribedStreamId(streamId string) {
	s.subscribedStreamIds = append(s.subscribedStreamIds, streamId)
	slog.Debug("Subscribed stream ID added", "sessionId", s.ID(), "streamId", streamId)
}

// removeSubscribedStreamId 구독 스트림 ID 제거
func (s *session) removeSubscribedStreamId(streamId string) {
	for i, id := range s.subscribedStreamIds {
		if id == streamId {
			s.subscribedStreamIds = append(s.subscribedStreamIds[:i], s.subscribedStreamIds[i+1:]...)
			slog.Debug("Subscribed stream ID removed", "sessionId", s.ID(), "streamId", streamId)
			break
		}
	}
}

// clearSubscribedStreamIds 모든 구독 스트림 ID 제거
func (s *session) clearSubscribedStreamIds() {
	s.subscribedStreamIds = s.subscribedStreamIds[:0]
	slog.Debug("All subscribed stream IDs cleared", "sessionId", s.ID())
}

// hasPublishedStreams 발행 스트림 보유 여부 확인
func (s *session) hasPublishedStreams() bool {
	return len(s.publishedStreams) > 0
}

// isPublishingMode 발행 모드 여부 확인
func (s *session) isPublishingMode() bool {
	return len(s.publishedStreams) > 0
}

// isPlayingMode 재생 모드 여부 확인
func (s *session) isPlayingMode() bool {
	return !s.isPublishingMode()
}

// --- MediaSource 인터페이스 구현 (발행자 모드) ---

// PublishingStreams MediaSource 인터페이스 구현 - 발행 중인 스트림 목록 반환
func (s *session) PublishingStreams() []*media.Stream {
	if s.isPublishingMode() {
		return s.getAllPublishedStreams()
	}
	return nil
}

// SubscribedStreams MediaSink 인터페이스 구현 - 구독 중인 스트림 ID 목록 반환
func (s *session) SubscribedStreams() []string {
	if s.isPlayingMode() {
		if len(s.subscribedStreamIds) > 0 {
			result := make([]string, len(s.subscribedStreamIds))
			copy(result, s.subscribedStreamIds)
			return result
		}
	}
	return nil
}

// --- 스트림 ID 관리 헬퍼 메서드들 ---

// generateStreamId 새로운 streamId 생성
func (s *session) generateStreamId() int {
	streamId := s.nextStreamId
	s.nextStreamId++
	return streamId
}

// addStreamMapping streamId와 streamPath 매핑 추가
func (s *session) addStreamMapping(streamId int, streamPath string) {
	s.streamIdToPath[streamId] = streamPath
	s.streamPathToId[streamPath] = streamId
}

// removeStreamMapping streamId 매핑 제거
func (s *session) removeStreamMapping(streamId int) {
	if streamPath, exists := s.streamIdToPath[streamId]; exists {
		delete(s.streamIdToPath, streamId)
		delete(s.streamPathToId, streamPath)
	}
}

// getStreamPathById streamId로 streamPath 조회
func (s *session) getStreamPathById(streamId int) (string, bool) {
	streamPath, exists := s.streamIdToPath[streamId]
	return streamPath, exists
}

// getStreamIdByPath streamPath로 streamId 조회
func (s *session) getStreamIdByPath(streamPath string) (int, bool) {
	streamId, exists := s.streamPathToId[streamPath]
	return streamId, exists
}

// --- StreamKey collision detection 메서드 ---

// attemptStreamPublish MediaServer에 publish 시도 (collision detection + 원자적 점유)
func (s *session) attemptStreamPublish(streamKey string, stream *media.Stream) bool {
	// MediaServer에 publish 시도 요청
	responseChan := make(chan media.Response, 1)
	publishAttempt := media.NewPublishStarted(s.ID(), media.NodeTypeRTMP, stream, responseChan)

	select {
	case s.mediaServerChannel <- publishAttempt:
		// 응답 대기 (타임아웃 5초)
		select {
		case response := <-responseChan:
			if response.Success {
				slog.Info("Stream publish attempt succeeded", "sessionId", s.ID(), "streamKey", streamKey)
				return true
			} else {
				slog.Info("Stream publish attempt failed", "sessionId", s.ID(), "streamKey", streamKey, "error", response.Error)
				return false
			}
		case <-time.After(5 * time.Second):
			slog.Error("Stream publish attempt timeout", "sessionId", s.ID(), "streamKey", streamKey)
			return false
		}
	case <-s.ctx.Done():
		return false
	}
}

// sendPublishErrorResponse publish 에러 응답 전송
func (s *session) sendPublishErrorResponse(transactionID float64, code string, description string) {
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

	err = s.writer.writeCommand(s.conn, statusSequence)
	if err != nil {
		slog.Error("Failed to write publish error response", "sessionId", s.ID(), "err", err)
	}
}

// sendPublishSuccessResponse publish 성공 응답 전송
func (s *session) sendPublishSuccessResponse(transactionID float64, streamPath string) {
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

	err = s.writer.writeCommand(s.conn, statusSequence)
	if err != nil {
		slog.Error("Failed to write publish success response", "sessionId", s.ID(), "err", err)
	}
}

// handleFCUnpublish FCUnpublish 명령어 처리
func (s *session) handleFCUnpublish(values []any) {

	// 발행 중단 처리
	if s.isPublishingMode() {
		s.stopPublishing()
	}
}

// handleCloseStream closeStream 명령어 처리
func (s *session) handleCloseStream(values []any) {

	// deleteStream과 동일한 처리
	if s.isPublishingMode() {
		s.stopPublishing()
	}

	s.stopPlaying() // 상태 체크 없이 항상 호출

	// 다중 스트림 정리
	s.publishedStreams = make(map[int]*media.Stream)
	s.streamIdToPath = make(map[int]string)
	s.streamPathToId = make(map[string]int)
	s.subscribedStreamIds = s.subscribedStreamIds[:0]
}

// handleDeleteStream deleteStream 명령어 처리
func (s *session) handleDeleteStream(values []any) {

	// 발행 중이었다면 발행 중단 이벤트 전송
	if s.isPublishingMode() {
		s.stopPublishing()
	}

	// 재생 중단 이벤트 전송 (상태 체크 없이)
	s.stopPlaying()

	// 다중 스트림 정리
	s.publishedStreams = make(map[int]*media.Stream)
	s.streamIdToPath = make(map[int]string)
	s.streamPathToId = make(map[string]int)
	s.subscribedStreamIds = s.subscribedStreamIds[:0]
}
