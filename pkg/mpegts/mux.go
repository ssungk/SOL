package mpegts

import (
	"context"
	"log/slog"
	"sol/pkg/media"
	"sync"
	"time"
)

// muxer MPEGTS 먹서 구현
type muxer struct {
	ctx    context.Context
	config MuxerConfig

	// 스트림 관리
	streams          map[uint16]*muxerStream // PID별 스트림 정보
	streamsMutex     sync.RWMutex
	nextStreamPID    uint16 // 다음에 할당할 스트림 PID
	
	// 카운터 관리
	patVersion        uint8  // PAT 버전
	pmtVersion        uint8  // PMT 버전
	continuityCounters map[uint16]uint8 // PID별 연속성 카운터
	counterMutex       sync.RWMutex

	// 타임스탬프 관리
	baseTimestamp uint64 // 기준 타임스탬프 (마이크로초)
	clockMutex    sync.RWMutex
}

// muxerStream 스트림 정보
type muxerStream struct {
	PID         uint16              // 스트림 PID
	StreamType  StreamType          // 스트림 타입
	CodecData   *CodecData         // 코덱 설정 데이터
	LastPTS     uint64             // 마지막 PTS
	LastDTS     uint64             // 마지막 DTS
	FrameCount  uint64             // 프레임 카운터
}

// newMuxer 새 먹서 생성
func newMuxer(ctx context.Context, config MuxerConfig) Muxer {
	// 기본값 설정
	if config.PMT_PID == 0 {
		config.PMT_PID = 0x100
	}
	if config.TransportStreamID == 0 {
		config.TransportStreamID = 0x0001
	}
	if config.ProgramNumber == 0 {
		config.ProgramNumber = 0x0001
	}

	m := &muxer{
		ctx:                ctx,
		config:            config,
		streams:           make(map[uint16]*muxerStream),
		nextStreamPID:     0x101, // 0x100은 PMT용으로 예약
		continuityCounters: make(map[uint16]uint8),
		baseTimestamp:     uint64(time.Now().UnixMicro()),
	}

	return m
}

// AddStream 스트림 추가
func (m *muxer) AddStream(streamType StreamType, codecData *CodecData) (uint16, error) {
	m.streamsMutex.Lock()
	defer m.streamsMutex.Unlock()

	// 새 PID 할당
	pid := m.nextStreamPID
	m.nextStreamPID++

	// 스트림 생성
	stream := &muxerStream{
		PID:        pid,
		StreamType: streamType,
		CodecData:  codecData,
	}

	m.streams[pid] = stream

	// PCR PID 설정 (첫 번째 비디오 스트림을 PCR PID로 사용)
	if m.config.PCR_PID == 0 && streamType.IsVideoStream() {
		m.config.PCR_PID = pid
	}

	slog.Info("Stream added to muxer", 
		"streamType", streamType.String(), 
		"PID", pid, 
		"pcrPID", m.config.PCR_PID)

	return pid, nil
}

// WriteFrame 프레임 쓰기
func (m *muxer) WriteFrame(streamPID uint16, frame media.MediaFrame) ([]byte, error) {
	m.streamsMutex.RLock()
	stream, exists := m.streams[streamPID]
	m.streamsMutex.RUnlock()

	if !exists {
		return nil, ErrUnsupportedStream
	}

	// 프레임 타임스탬프를 PTS/DTS로 변환
	pts := m.frameTimestampToPTS(frame.Timestamp)
	dts := pts // 기본적으로 DTS = PTS

	// 비디오 프레임의 경우 DTS를 약간 앞서게 설정할 수 있음
	if frame.IsVideo() && frame.Type == media.TypeData {
		// B프레임이 있는 경우 DTS를 조정해야 하지만, 여기서는 간단히 처리
		dts = pts - 3003 // 약 33ms 앞서게 (29.97fps 기준 1프레임)
	}

	// 프레임 데이터 사용 (이미 []byte)
	frameData := frame.Data

	// PES 패킷 생성
	var streamID uint8
	if frame.Codec.IsVideo() {
		streamID = 0xE0 // 첫 번째 비디오 스트림
	} else if frame.Codec.IsAudio() {
		streamID = 0xC0 // 첫 번째 오디오 스트림
	} else {
		streamID = 0xBD // 개인 스트림
	}

	pesData, err := createPESPacket(streamID, pts, dts, frameData)
	if err != nil {
		return nil, err
	}

	// PES 패킷을 TS 패킷들로 분할
	var tsPackets []byte

	pesOffset := 0
	firstPacket := true

	for pesOffset < len(pesData) {
		// TS 패킷 헤더 생성
		header := TSPacketHeader{
			SyncByte:                  SyncByte,
			TransportErrorIndicator:   false,
			PayloadUnitStartIndicator: firstPacket,
			TransportPriority:         false,
			PID:                       streamPID,
			TransportScramblingControl: 0,
			AdaptationFieldControl:    0x01, // 페이로드만
			ContinuityCounter:         m.getAndIncrementCC(streamPID),
		}

		// 적응 필드 (PCR 추가)
		var adaptationField *AdaptationField
		if streamPID == m.config.PCR_PID && firstPacket {
			// PCR 추가
			adaptationField = &AdaptationField{
				Length:                1,
				RandomAccessIndicator: frame.Type == media.TypeKey,
				PCRFlag:               true,
				PCR:                   m.getCurrentPCR(),
			}
			header.AdaptationFieldControl = 0x03 // 적응 필드 + 페이로드
		}

		// 페이로드 크기 계산
		availablePayloadSize := TSPacketSize - 4 // 헤더 크기
		if adaptationField != nil {
			availablePayloadSize -= int(adaptationField.Length) + 1
		}

		// 페이로드 추출
		remainingPESData := len(pesData) - pesOffset
		payloadSize := availablePayloadSize
		if payloadSize > remainingPESData {
			payloadSize = remainingPESData
		}

		payload := pesData[pesOffset : pesOffset+payloadSize]

		// TS 패킷 생성
		packet, err := createTSPacket(header, adaptationField, payload)
		if err != nil {
			return nil, err
		}

		tsPackets = append(tsPackets, packet...)

		pesOffset += payloadSize
		firstPacket = false
	}

	// 스트림 정보 업데이트
	m.streamsMutex.Lock()
	stream.LastPTS = pts
	stream.LastDTS = dts
	stream.FrameCount++
	m.streamsMutex.Unlock()

	return tsPackets, nil
}

// WriteMetadata 메타데이터 쓰기
func (m *muxer) WriteMetadata(streamPID uint16, metadata map[string]string) ([]byte, error) {
	// 간단한 구현: 메타데이터를 개인 데이터로 인코딩
	slog.Debug("Metadata written to muxer", "streamPID", streamPID, "metadata", metadata)
	return nil, nil
}

// GetPAT PAT 테이블 생성
func (m *muxer) GetPAT() ([]byte, error) {
	programs := []PATEntry{
		{
			ProgramNumber: m.config.ProgramNumber,
			PID:           m.config.PMT_PID,
		},
	}

	patData, err := createPAT(m.config.TransportStreamID, programs)
	if err != nil {
		return nil, err
	}

	// PAT를 TS 패킷으로 래핑
	header := TSPacketHeader{
		SyncByte:                  SyncByte,
		TransportErrorIndicator:   false,
		PayloadUnitStartIndicator: true,
		TransportPriority:         false,
		PID:                       PIDPAT,
		TransportScramblingControl: 0,
		AdaptationFieldControl:    0x01,
		ContinuityCounter:         m.getAndIncrementCC(PIDPAT),
	}

	packet, err := createTSPacket(header, nil, patData)
	if err != nil {
		return nil, err
	}

	return packet, nil
}

// GetPMT PMT 테이블 생성
func (m *muxer) GetPMT(programNumber uint16) ([]byte, error) {
	m.streamsMutex.RLock()
	defer m.streamsMutex.RUnlock()

	var streams []PMTStreamInfo
	for pid, stream := range m.streams {
		pmtStream := PMTStreamInfo{
			StreamType:    stream.StreamType,
			ElementaryPID: pid,
			ESInfoLength:  0,
			Descriptors:   nil,
		}
		streams = append(streams, pmtStream)
	}

	pmtData, err := createPMT(programNumber, m.config.PCR_PID, streams)
	if err != nil {
		return nil, err
	}

	// PMT를 TS 패킷으로 래핑
	header := TSPacketHeader{
		SyncByte:                  SyncByte,
		TransportErrorIndicator:   false,
		PayloadUnitStartIndicator: true,
		TransportPriority:         false,
		PID:                       m.config.PMT_PID,
		TransportScramblingControl: 0,
		AdaptationFieldControl:    0x01,
		ContinuityCounter:         m.getAndIncrementCC(m.config.PMT_PID),
	}

	packet, err := createTSPacket(header, nil, pmtData)
	if err != nil {
		return nil, err
	}

	return packet, nil
}

// Close 리소스 정리
func (m *muxer) Close() error {
	return nil
}

// getAndIncrementCC 연속성 카운터 가져오기 및 증가
func (m *muxer) getAndIncrementCC(pid uint16) uint8 {
	m.counterMutex.Lock()
	defer m.counterMutex.Unlock()

	cc := m.continuityCounters[pid]
	m.continuityCounters[pid] = (cc + 1) & 0x0F // 4비트로 제한
	return cc
}

// frameTimestampToPTS 프레임 타임스탬프를 PTS로 변환
func (m *muxer) frameTimestampToPTS(frameTimestampMs uint32) uint64 {
	// 프레임 타임스탬프(ms)를 PTS(90kHz 클록)로 변환
	return uint64(frameTimestampMs) * 90
}

// getCurrentPCR 현재 PCR 값 생성
func (m *muxer) getCurrentPCR() uint64 {
	m.clockMutex.RLock()
	defer m.clockMutex.RUnlock()

	// 현재 시간을 기준으로 PCR 생성 (27MHz 클록 기준)
	nowUs := uint64(time.Now().UnixMicro())
	elapsedUs := nowUs - m.baseTimestamp
	return elapsedUs * 27
}

// GeneratePATAndPMT PAT와 PMT 패킷들을 생성
func (m *muxer) GeneratePATAndPMT() ([]byte, error) {
	var result []byte

	// PAT 생성
	pat, err := m.GetPAT()
	if err != nil {
		return nil, err
	}
	result = append(result, pat...)

	// PMT 생성
	pmt, err := m.GetPMT(m.config.ProgramNumber)
	if err != nil {
		return nil, err
	}
	result = append(result, pmt...)

	return result, nil
}

// GetStreamInfo 스트림 정보 반환
func (m *muxer) GetStreamInfo() map[uint16]StreamInfo {
	m.streamsMutex.RLock()
	defer m.streamsMutex.RUnlock()

	info := make(map[uint16]StreamInfo)
	for pid, stream := range m.streams {
		info[pid] = StreamInfo{
			PID:       pid,
			Type:      stream.StreamType,
			CodecData: stream.CodecData,
		}
	}

	return info
}