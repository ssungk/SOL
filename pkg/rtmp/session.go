package rtmp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/amf"
)

type session struct {
	reader          *messageReader
	writer          *messageWriter
	conn            net.Conn
	externalChannel chan<- interface{}
	messageChannel  chan *Message

	// Session 식별자 - 포인터 주소값 기반
	sessionId string

	// Stream 관리
	streamID     uint32
	streamName   string // streamkey
	appName      string // appname
	isPublishing bool
	isPlaying    bool
}

// GetID는 세션의 ID를 반환 (sessionId 필드)
func (s *session) GetID() string {
	return s.sessionId
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

// publish 명령어 처리
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
	s.isPublishing = true

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Error("publish: invalid stream path", "appName", s.appName, "streamName", streamName)
		return
	}

	slog.Info("publish request", "fullStreamPath", fullStreamPath, "publishType", publishType, "transactionID", transactionID)

	// Publish 시작 이벤트 전송
	s.sendEvent(PublishStarted{
		SessionId:  s.sessionId,
		StreamName: fullStreamPath, // full path 사용
		StreamId:   s.streamID,
	})

	// onStatus 이벤트 전송: NetStream.Publish.Start
	statusObj := map[string]any{
		"level":       "status",
		"code":        "NetStream.Publish.Start",
		"description": fmt.Sprintf("Started publishing stream %s", fullStreamPath),
		"details":     fullStreamPath,
	}

	// onStatus 이벤트 전송 (transaction ID는 0)
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

// handlePlay의 transactionID 사용
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
	s.isPlaying = true

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Error("play: invalid stream path", "appName", s.appName, "streamName", streamName)
		return
	}

	slog.Info("play request", "fullStreamPath", fullStreamPath, "transactionID", transactionID)

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
		StreamName: fullStreamPath, // full path 사용
		StreamId:   s.streamID,
	})

	slog.Info("play started successfully", "fullStreamPath", fullStreamPath, "transactionID", transactionID)
}

// releaseStream 명령어 처리
func (s *session) handleReleaseStream(values []any) {
	slog.Info("handling releaseStream", "params", values)

	if len(values) < 3 {
		slog.Error("releaseStream: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("releaseStream: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("releaseStream: invalid stream name", "type", fmt.Sprintf("%T", values[3]))
		return
	}

	slog.Info("releaseStream request", "streamName", streamName, "transactionID", transactionID)

	// _result 응답 전송
	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, nil)
	if err != nil {
		slog.Error("releaseStream: failed to encode response", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, sequence)
	if err != nil {
		slog.Error("releaseStream: failed to write response", "err", err)
		return
	}

	slog.Info("releaseStream successful", "streamName", streamName, "transactionID", transactionID)
}

// FCPublish 명령어 처리
func (s *session) handleFCPublish(values []any) {
	slog.Info("handling FCPublish", "params", values)

	if len(values) < 3 {
		slog.Error("FCPublish: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("FCPublish: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("FCPublish: invalid stream name", "type", fmt.Sprintf("%T", values[3]))
		return
	}

	slog.Info("FCPublish request", "streamName", streamName, "transactionID", transactionID)

	// 1. _result 응답 전송
	resultSequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, nil)
	if err != nil {
		slog.Error("FCPublish: failed to encode _result", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, resultSequence)
	if err != nil {
		slog.Error("FCPublish: failed to write _result", "err", err)
		return
	}

	// 2. onFCPublish 이벤트 전송
	fcPublishObj := map[string]any{
		"code":        "NetStream.Publish.Start",
		"description": fmt.Sprintf("FCPublish to stream %s", streamName),
	}

	onFCPublishSequence, err := amf.EncodeAMF0Sequence("onFCPublish", 0.0, nil, fcPublishObj)
	if err != nil {
		slog.Error("FCPublish: failed to encode onFCPublish", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, onFCPublishSequence)
	if err != nil {
		slog.Error("FCPublish: failed to write onFCPublish", "err", err)
		return
	}

	slog.Info("FCPublish successful", "streamName", streamName, "transactionID", transactionID)
}

// FCUnpublish 명령어 처리
func (s *session) handleFCUnpublish(values []any) {
	slog.Info("handling FCUnpublish", "params", values)

	if len(values) < 3 {
		slog.Error("FCUnpublish: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("FCUnpublish: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	streamName, ok := values[3].(string)
	if !ok {
		slog.Error("FCUnpublish: invalid stream name", "type", fmt.Sprintf("%T", values[3]))
		return
	}

	slog.Info("FCUnpublish request", "streamName", streamName, "transactionID", transactionID)

	// 1. _result 응답 전송 (SRS 스타일)
	resultSequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, nil)
	if err != nil {
		slog.Error("FCUnpublish: failed to encode _result", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, resultSequence)
	if err != nil {
		slog.Error("FCUnpublish: failed to write _result", "err", err)
		return
	}

	// FCUnpublish 는 publish 종료를 예고하는 명령어이므로 별도 처리가 필요할 수 있음
	fullStreamPath := s.GetFullStreamPath()
	// Publish 종료 이벤트 전송 (FCUnpublish는 publish 종료를 의미)
	if s.isPublishing && fullStreamPath != "" {
		s.sendEvent(PublishStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
		s.isPublishing = false
	}

	// 2. onFCUnpublish 이벤트 전송
	fcUnpublishObj := map[string]any{
		"code":        "NetStream.Unpublish.Success",
		"description": fmt.Sprintf("FCUnpublish to stream %s", streamName),
	}

	onFCUnpublishSequence, err := amf.EncodeAMF0Sequence("onFCUnpublish", 0.0, nil, fcUnpublishObj)
	if err != nil {
		slog.Error("FCUnpublish: failed to encode onFCUnpublish", "err", err)
		return
	}

	err = s.writer.writeCommand(s.conn, onFCUnpublishSequence)
	if err != nil {
		slog.Error("FCUnpublish: failed to write onFCUnpublish", "err", err)
		return
	}

	slog.Info("FCUnpublish successful", "streamName", streamName, "transactionID", transactionID)
}

// closeStream 명령어 처리
func (s *session) handleCloseStream(values []any) {
	slog.Info("handling closeStream", "params", values)

	fullStreamPath := s.GetFullStreamPath()
	// 이벤트 전송
	if s.isPublishing && fullStreamPath != "" {
		s.sendEvent(PublishStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
	}
	if s.isPlaying && fullStreamPath != "" {
		s.sendEvent(PlayStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
	}

	s.isPublishing = false
	s.isPlaying = false

	slog.Info("stream closed", "fullStreamPath", fullStreamPath)
}

// deleteStream 명령어 처리
func (s *session) handleDeleteStream(values []any) {
	slog.Info("handling deleteStream", "params", values)

	if len(values) < 3 {
		slog.Error("deleteStream: not enough parameters", "length", len(values))
		return
	}

	streamID, ok := values[3].(float64)
	if !ok {
		slog.Error("deleteStream: invalid stream ID", "type", fmt.Sprintf("%T", values[3]))
		return
	}

	fullStreamPath := s.GetFullStreamPath()
	// 이벤트 전송
	if s.isPublishing && fullStreamPath != "" {
		s.sendEvent(PublishStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
	}
	if s.isPlaying && fullStreamPath != "" {
		s.sendEvent(PlayStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
	}

	s.isPublishing = false
	s.isPlaying = false

	slog.Info("stream deleted", "streamID", streamID, "fullStreamPath", fullStreamPath)
}

// pause 명령어 처리
func (s *session) handlePause(values []any) {
	slog.Info("handling pause", "params", values)

	if len(values) < 4 {
		slog.Error("pause: not enough parameters", "length", len(values))
		return
	}

	pauseFlag, ok := values[3].(bool)
	if !ok {
		slog.Error("pause: invalid pause flag", "type", fmt.Sprintf("%T", values[3]))
		return
	}

	if pauseFlag {
		slog.Info("stream paused")
	} else {
		slog.Info("stream resumed")
	}
}

// receiveAudio 명령어 처리
func (s *session) handleReceiveAudio(values []any) {
	slog.Info("handling receiveAudio", "params", values)
}

// receiveVideo 명령어 처리
func (s *session) handleReceiveVideo(values []any) {
	slog.Info("handling receiveVideo", "params", values)
}

// onBWDone 명령어 처리
func (s *session) handleOnBWDone(values []any) {
	slog.Info("handling onBWDone", "params", values)
}

// 오디오 데이터 처리
func (s *session) handleAudio(message *Message) {
	if !s.isPublishing {
		slog.Warn("received audio data but not publishing")
		return
	}

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Warn("received audio data but no valid stream path")
		return
	}

	// 오디오 데이터를 그대로 전달 (FLV 형식 유지)
	audioData := make([]byte, 0)
	for _, chunk := range message.payload {
		audioData = append(audioData, chunk...)
	}

	if len(audioData) == 0 {
		slog.Warn("empty audio data received")
		return
	}

	firstByte := audioData[0]
	codecId := "unknown"
	sampleRate := "unknown"
	sampleSize := "unknown"
	channels := "unknown"
	aacPacketType := ""

	// 오디오 코덱 ID (4비트)
	switch (firstByte >> 4) & 0x0F {
	case 0:
		codecId = "Linear PCM, platform endian"
	case 1:
		codecId = "ADPCM"
	case 2:
		codecId = "MP3"
	case 3:
		codecId = "Linear PCM, little endian"
	case 4:
		codecId = "Nellymoser 16kHz mono"
	case 5:
		codecId = "Nellymoser 8kHz mono"
	case 6:
		codecId = "Nellymoser"
	case 7:
		codecId = "G.711 A-law"
	case 8:
		codecId = "G.711 mu-law"
	case 10:
		codecId = "AAC"
	case 11:
		codecId = "Speex"
	case 14:
		codecId = "MP3 8kHz"
	case 15:
		codecId = "Device-specific sound"
	}

	// 샘플링 비율 (2비트)
	switch (firstByte >> 2) & 0x03 {
	case 0:
		sampleRate = "5.5kHz"
	case 1:
		sampleRate = "11kHz"
	case 2:
		sampleRate = "22kHz"
	case 3:
		sampleRate = "44kHz"
	}

	// 샘플 크기 (1비트)
	if (firstByte>>1)&0x01 == 0 {
		sampleSize = "8-bit"
	} else {
		sampleSize = "16-bit"
	}

	// 채널 (1비트)
	if firstByte&0x01 == 0 {
		channels = "mono"
	} else {
		channels = "stereo"
	}

	// AAC 특수 처리
	if ((firstByte>>4)&0x0F) == 10 && len(audioData) > 1 {
		aacPacketType = ""
		switch audioData[1] {
		case 0:
			aacPacketType = "AAC sequence header" // AudioSpecificConfig
		case 1:
			aacPacketType = "AAC raw" // 실제 오디오 데이터
		}

		if audioData[1] == 0 {
			slog.Info("received AAC sequence header",
				"dataSize", len(audioData),
				"timestamp", message.messageHeader.Timestamp)
		}
	}

	slog.Debug("received audio data",
		"fullStreamPath", fullStreamPath,
		"dataSize", len(audioData),
		"codecId", codecId,
		"sampleRate", sampleRate,
		"sampleSize", sampleSize,
		"channels", channels,
		"aacPacketType", aacPacketType,
		"timestamp", message.messageHeader.Timestamp,
		"firstByte", fmt.Sprintf("0x%02x", firstByte))

	// 오디오 데이터 이벤트 전송
	s.sendEvent(AudioData{
		SessionId:  s.sessionId,
		StreamName: fullStreamPath,
		Timestamp:  message.messageHeader.Timestamp,
		Data:       audioData,
	})
}

// 비디오 데이터 처리
func (s *session) handleVideo(message *Message) {
	if !s.isPublishing {
		slog.Warn("received video data but not publishing")
		return
	}

	fullStreamPath := s.GetFullStreamPath()
	if fullStreamPath == "" {
		slog.Warn("received video data but no valid stream path")
		return
	}

	// 비디오 데이터를 그대로 전달 (FLV 형식 유지)
	videoData := make([]byte, 0)
	for _, chunk := range message.payload {
		videoData = append(videoData, chunk...)
	}

	if len(videoData) == 0 {
		slog.Warn("empty video data received")
		return
	}

	// 비디오 프레임 타입 확인 (첫 번째 바이트)
	firstByte := videoData[0]
	frameType := "unknown"
	codecId := "unknown"

	// 프레임 타입 (4비트)
	switch (firstByte >> 4) & 0x0F {
	case 1:
		frameType = "key frame"
	case 2:
		frameType = "inter frame"
	case 3:
		frameType = "disposable inter frame"
	case 4:
		frameType = "generated key frame"
	case 5:
		frameType = "video info/command frame"
	}

	// 코덱 ID (4비트)
	switch firstByte & 0x0F {
	case 2:
		codecId = "Sorenson H.263"
	case 3:
		codecId = "Screen video"
	case 4:
		codecId = "On2 VP6"
	case 5:
		codecId = "On2 VP6 with alpha"
	case 6:
		codecId = "Screen video version 2"
	case 7:
		codecId = "AVC (H.264)"
	}

	// H.264 특수 처리
	if (firstByte&0x0F) == 7 && len(videoData) > 1 {
		avcPacketType := videoData[1]
		switch avcPacketType {
		case 0:
			frameType = "AVC sequence header" // SPS/PPS
		case 1:
			frameType = "AVC NALU" // 실제 비디오 데이터
		case 2:
			frameType = "AVC end of sequence"
		}

		// AVC sequence header인 경우 세부 정보 로깅
		if avcPacketType == 0 {
			slog.Info("received AVC sequence header",
				"dataSize", len(videoData),
				"timestamp", message.messageHeader.Timestamp)

			// SPS/PPS 데이터 분석 (선택적)
			if len(videoData) > 10 {
				configurationVersion := videoData[5]
				profile := videoData[6]
				compatibility := videoData[7]
				level := videoData[8]

				slog.Info("AVC configuration",
					"version", configurationVersion,
					"profile", profile,
					"compatibility", compatibility,
					"level", level)
			}
		}
	}

	slog.Debug("received video data",
		"fullStreamPath", fullStreamPath,
		"dataSize", len(videoData),
		"frameType", frameType,
		"codecId", codecId,
		"timestamp", message.messageHeader.Timestamp,
		"firstByte", fmt.Sprintf("0x%02x", firstByte))

	// 비디오 데이터 이벤트 전송
	s.sendEvent(VideoData{
		SessionId:  s.sessionId,
		StreamName: fullStreamPath,
		Timestamp:  message.messageHeader.Timestamp,
		FrameType:  frameType,
		Data:       videoData,
	})
}

// 스크립트 데이터 처리 (메타데이터 등)
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

// 메타데이터 처리
func (s *session) handleOnMetaData(values []any) {
	slog.Info("received onMetaData")

	if len(values) < 2 {
		slog.Warn("onMetaData: insufficient data")
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

	// 주요 메타데이터 정보 로깅
	if width, ok := metadata["width"]; ok {
		slog.Info("video width", "width", width)
	}
	if height, ok := metadata["height"]; ok {
		slog.Info("video height", "height", height)
	}
	if framerate, ok := metadata["framerate"]; ok {
		slog.Info("video framerate", "framerate", framerate)
	}
	if videoBitrate, ok := metadata["videodatarate"]; ok {
		slog.Info("video bitrate", "bitrate", videoBitrate)
	}
	if audioBitrate, ok := metadata["audiodatarate"]; ok {
		slog.Info("audio bitrate", "bitrate", audioBitrate)
	}
	if audioSampleRate, ok := metadata["audiosamplerate"]; ok {
		slog.Info("audio sample rate", "sampleRate", audioSampleRate)
	}
	if duration, ok := metadata["duration"]; ok {
		slog.Info("video duration", "duration", duration)
	}
	if audiocodecid, ok := metadata["audiocodecid"]; ok {
		slog.Info("audio codec", "codecid", audiocodecid)
	}
	if videocodecid, ok := metadata["videocodecid"]; ok {
		slog.Info("video codec", "codecid", videocodecid)
	}

	// 메타데이터 이벤트 전송
	s.sendEvent(MetaData{
		SessionId:  s.sessionId,
		StreamName: fullStreamPath,
		Metadata:   metadata,
	})

	slog.Info("metadata processed successfully", "fullStreamPath", fullStreamPath, "metadataKeys", len(metadata))
}

// 텍스트 데이터 처리
func (s *session) handleOnTextData(values []any) {
	slog.Info("received onTextData", "values", values)

	// TODO: 텍스트 데이터 처리
}

// GetFullStreamPath는 appname/streamkey 조합의 전체 스트림 경로를 반환
func (s *session) GetFullStreamPath() string {
	if s.appName == "" || s.streamName == "" {
		return ""
	}
	return s.appName + "/" + s.streamName
}

// GetStreamInfo는 세션 정보를 반환
func (s *session) GetStreamInfo() (streamID uint32, streamName string, isPublishing bool, isPlaying bool) {
	return s.streamID, s.streamName, s.isPublishing, s.isPlaying
}

// 세션 정리
func (s *session) cleanup() {
	fullStreamPath := s.GetFullStreamPath()
	// Publish/Play 종료 이벤트 전송
	if s.isPublishing && fullStreamPath != "" {
		s.sendEvent(PublishStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
	}
	if s.isPlaying && fullStreamPath != "" {
		s.sendEvent(PlayStopped{
			SessionId:  s.sessionId,
			StreamName: fullStreamPath,
			StreamId:   s.streamID,
		})
	}

	s.isPublishing = false
	s.isPlaying = false
	s.streamID = 0
	s.streamName = ""
	s.appName = ""

	slog.Info("session cleanup completed", "sessionId", s.sessionId, "fullStreamPath", fullStreamPath)
}

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

func (s *session) handleRead() {
	defer func() {
		s.cleanup()
		closeWithLog(s.conn)
	}()

	if err := handshake(s.conn); err != nil {
		slog.Info("Handshake failed:", "err", err)
		return
	}

	slog.Info("Handshake successful with", "addr", s.conn.RemoteAddr())

	for {
		slog.Info("loop")
		message, err := s.reader.readNextMessage(s.conn)
		if err != nil {
			return
		}

		switch message.messageHeader.typeId {
		case MSG_TYPE_SET_CHUNK_SIZE: // Set Chunk Size
			s.handleSetChunkSize(message)
		default:
			s.handleMessage(message)
			//s.messageChannel <- message
		}
	}
}

func (s *session) handleEvent() {
	for {
		select {
		case message := <-s.messageChannel:
			s.handleMessage(message)
		}
	}
}

func (s *session) handleMessage(message *Message) {
	slog.Info("receive message", "typeId", message.messageHeader.typeId)
	switch message.messageHeader.typeId {
	case MSG_TYPE_SET_CHUNK_SIZE: // Set Chunk Size
		s.handleSetChunkSize(message)
	case MSG_TYPE_ABORT: // Abort Message
		// Optional: ignore or log
	case MSG_TYPE_ACKNOWLEDGEMENT: // Acknowledgement
		// 서버용: 클라이언트의 ack 수신
	case MSG_TYPE_USER_CONTROL: // User Control Messages
		//s.handleUserControl(message)
	case MSG_TYPE_WINDOW_ACK_SIZE: // Window Acknowledgement Size
		// 클라이언트가 설정한 ack 윈도우 크기
	case MSG_TYPE_SET_PEER_BW: // Set Peer Bandwidth
		// bandwidth 제한에 대한 정보
	case MSG_TYPE_AUDIO: // Audio
		s.handleAudio(message)
	case MSG_TYPE_VIDEO: // Video
		s.handleVideo(message)
	case MSG_TYPE_AMF3_DATA: // AMF3 Data Message
		// AMF3 포맷. 대부분 Flash Player
	case MSG_TYPE_AMF3_SHARED_OBJECT: // AMF3 Shared Object
	case MSG_TYPE_AMF3_COMMAND: // AMF3 Command Message
	case MSG_TYPE_AMF0_DATA: // AMF0 Data Message (e.g., onMetaData)
		s.handleScriptData(message)
	case MSG_TYPE_AMF0_COMMAND: // AMF0 Command Message (e.g., connect, play, publish)
		s.handleAMF0Command(message)
	default:
		slog.Warn("unhandled RTMP message type", "type", message.messageHeader.typeId)
	}
}

func (s *session) handleSetChunkSize(message *Message) {
	slog.Info("handleSetChunkSize")

	if len(message.payload[0]) != 4 {
		slog.Error("Invalid Set Chunk Size message length", "length", len(message.payload))
		return
	}

	// 4바이트에서 uint32로 읽기 (big endian)
	newChunkSize := binary.BigEndian.Uint32(message.payload[0])

	// 첫 번째 비트(최상위 비트) 체크: 반드시 0이어야 함
	if newChunkSize&0x80000000 != 0 {
		slog.Error("Set Chunk Size has reserved highest bit set", "value", newChunkSize)
		// TODO: 에러 처리후 연결 종료??
		return
	}

	// RTMP 최대 청크 크기 제한 (1 ~ 16777215)
	if newChunkSize < 1 || newChunkSize > EXTENDED_TIMESTAMP_THRESHOLD {
		slog.Error("Set Chunk Size out of valid range", "value", newChunkSize)
		return
	}

	// 실제 세션 청크 크기 적용
	s.reader.setChunkSize(newChunkSize)
}

func (s *session) handleAMF0Command(message *Message) {
	slog.Info("handleAMF0Command")
	reader := ConcatByteSlicesReader(message.payload)
	values, err := amf.DecodeAMF0Sequence(reader)
	if err != nil {
		// TODO: handle error
	}
	for _, v := range values {
		slog.Info("amf", "value", v)
	}

	commandName, ok := values[0].(string)
	if !ok {
		slog.Error("Invalid command name type", "actual", fmt.Sprintf("%T", values[0]))
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
		slog.Error("Unknown AMF0 command", "name", commandName)
	}
}

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

func ConcatByteSlicesReader(slices [][]byte) io.Reader {
	readers := make([]io.Reader, 0, len(slices))
	for _, b := range slices {
		readers = append(readers, bytes.NewReader(b))
	}
	return io.MultiReader(readers...)
}
