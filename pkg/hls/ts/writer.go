package ts

import (
	"bytes"
	"fmt"
	"sol/pkg/media"
)

// TSWriter TS 패킷 작성기
type TSWriter struct {
	// PID 관리
	pmtPID   uint16 // PMT PID
	videoPID uint16 // 비디오 PID
	audioPID uint16 // 오디오 PID

	// 연속성 카운터
	continuityCounters map[uint16]uint8 // PID별 연속성 카운터

	// 프로그램 정보
	programNumber uint16 // 프로그램 번호
	pcrPID        uint16 // PCR PID

	// PCR 계산기
	pcrCalculator *PCRCalculator
	lastPCRTime   int64 // 마지막 PCR 시간

	// 출력 버퍼
	output *bytes.Buffer

	// 스트림 정보
	hasVideo   bool
	hasAudio   bool
	videoCodec string
	audioCodec string
}

// NewTSWriter 새 TS 작성기 생성
func NewTSWriter() *TSWriter {
	return &TSWriter{
		pmtPID:             PIDPMTStart,
		videoPID:           PIDVideoPrimary,
		audioPID:           PIDAudioPrimary,
		continuityCounters: make(map[uint16]uint8),
		programNumber:      1,
		pcrPID:             PIDVideoPrimary,
		pcrCalculator:      NewPCRCalculator(),
		output:             &bytes.Buffer{},
	}
}

// WriteSegment 세그먼트를 TS 형식으로 작성
func (w *TSWriter) WriteSegment(frames []media.Frame) ([]byte, error) {
	w.output.Reset()

	// 프레임 분석
	w.analyzeFrames(frames)

	// PAT/PMT 테이블 작성
	if err := w.writePAT(); err != nil {
		return nil, fmt.Errorf("failed to write PAT: %w", err)
	}

	if err := w.writePMT(); err != nil {
		return nil, fmt.Errorf("failed to write PMT: %w", err)
	}

	// 프레임들을 PES로 변환하여 작성
	for i, frame := range frames {
		if err := w.writeFrame(frame, i == 0); err != nil {
			return nil, fmt.Errorf("failed to write frame %d: %w", i, err)
		}
	}

	// 패킷 정렬을 위한 null 패킷 추가
	w.padWithNullPackets()

	return w.output.Bytes(), nil
}

// analyzeFrames 프레임들을 분석하여 스트림 정보 설정
func (w *TSWriter) analyzeFrames(frames []media.Frame) {
	w.hasVideo = false
	w.hasAudio = false

	for _, frame := range frames {
		switch frame.Type {
		case media.TypeVideo:
			w.hasVideo = true
			// H.264 가정
			w.videoCodec = "h264"
		case media.TypeAudio:
			w.hasAudio = true
			// AAC 가정
			w.audioCodec = "aac"
		}
	}

	// PCR PID 설정 (비디오가 있으면 비디오, 없으면 오디오)
	if w.hasVideo {
		w.pcrPID = w.videoPID
	} else if w.hasAudio {
		w.pcrPID = w.audioPID
	}
}

// writePAT PAT (Program Association Table) 작성
func (w *TSWriter) writePAT() error {
	patData := w.generatePAT()
	return w.writeTablePacket(PIDPatTable, patData, true)
}

// writePMT PMT (Program Map Table) 작성
func (w *TSWriter) writePMT() error {
	pmtData := w.generatePMT()
	return w.writeTablePacket(w.pmtPID, pmtData, true)
}

// writeFrame 프레임을 PES 패킷으로 작성
func (w *TSWriter) writeFrame(frame media.Frame, isFirst bool) error {
	var pid uint16
	var streamID uint8

	// PID와 스트림 ID 결정
	switch frame.Type {
	case media.TypeVideo:
		pid = w.videoPID
		streamID = PESVideoStreamID
	case media.TypeAudio:
		pid = w.audioPID
		streamID = PESAudioStreamID
	default:
		return nil // 지원하지 않는 타입 무시
	}

	// 프레임 데이터 병합
	frameData := w.mergeFrameData(frame.Data)

	// PES 패킷 생성
	pesData := w.generatePES(streamID, frameData, frame.Timestamp)

	// PCR 추가 (필요시)
	needPCR := isFirst || (frame.Type == media.TypeVideo && media.IsKeyFrame(frame.FrameType))

	// PES 데이터를 TS 패킷들로 분할
	return w.writePESPackets(pid, pesData, needPCR, frame.Timestamp)
}

// writeTablePacket 테이블 패킷 작성
func (w *TSWriter) writeTablePacket(pid uint16, data []byte, payloadStart bool) error {
	packet := make([]byte, TSPacketSize)

	// 헤더 작성
	w.writePacketHeader(packet, pid, payloadStart, false)

	// 페이로드 크기 계산
	payloadOffset := TSHeaderMinSize
	availableSpace := TSPacketSize - payloadOffset

	if payloadStart {
		// Pointer field 추가
		packet[payloadOffset] = 0x00
		payloadOffset++
		availableSpace--
	}

	// 데이터 복사
	copyLen := len(data)
	if copyLen > availableSpace {
		copyLen = availableSpace
	}

	copy(packet[payloadOffset:payloadOffset+copyLen], data[:copyLen])

	// 나머지 공간을 0xFF로 패딩
	for i := payloadOffset + copyLen; i < TSPacketSize; i++ {
		packet[i] = 0xFF
	}

	w.output.Write(packet)
	return nil
}

// writePESPackets PES 데이터를 여러 TS 패킷으로 분할하여 작성
func (w *TSWriter) writePESPackets(pid uint16, pesData []byte, needPCR bool, timestamp uint32) error {
	dataOffset := 0
	isFirst := true

	for dataOffset < len(pesData) {
		packet := make([]byte, TSPacketSize)

		// PCR 추가 여부 결정
		addPCR := needPCR && isFirst && pid == w.pcrPID

		// 헤더 및 adaptation field 작성
		payloadOffset := w.writePacketHeader(packet, pid, isFirst, addPCR)

		// PCR 작성
		if addPCR {
			payloadOffset = w.writePCR(packet, payloadOffset, int64(timestamp)*1000)
		}

		// 페이로드 복사
		availableSpace := TSPacketSize - payloadOffset
		copyLen := len(pesData) - dataOffset
		if copyLen > availableSpace {
			copyLen = availableSpace
		}

		copy(packet[payloadOffset:payloadOffset+copyLen], pesData[dataOffset:dataOffset+copyLen])

		// 나머지 공간을 0xFF로 패딩
		for i := payloadOffset + copyLen; i < TSPacketSize; i++ {
			packet[i] = 0xFF
		}

		w.output.Write(packet)

		dataOffset += copyLen
		isFirst = false
	}

	return nil
}

// writePacketHeader TS 패킷 헤더 작성
func (w *TSWriter) writePacketHeader(packet []byte, pid uint16, payloadStart, needAdaptation bool) int {
	// Sync byte
	packet[0] = TSSyncByte

	// 플래그들과 PID (상위 5비트)
	packet[1] = 0x00
	if payloadStart {
		packet[1] |= 0x40 // payload_unit_start_indicator
	}
	packet[1] |= uint8((pid >> 8) & 0x1F)

	// PID (하위 8비트)
	packet[2] = uint8(pid & 0xFF)

	// Adaptation field control과 continuity counter
	adaptationControl := AdaptationFieldNone
	if needAdaptation {
		adaptationControl = AdaptationFieldWithPayload
	}

	counter := w.getContinuityCounter(pid)
	packet[3] = (uint8(adaptationControl) << 4) | counter

	headerSize := TSHeaderMinSize

	// Adaptation field 작성 (필요시)
	if needAdaptation {
		// 기본적으로 1바이트 길이 필드만 추가
		packet[4] = 0x00 // adaptation field length = 0
		headerSize = 5
	}

	return headerSize
}

// writePCR PCR 필드 작성
func (w *TSWriter) writePCR(packet []byte, offset int, timestampUs int64) int {
	if w.pcrCalculator.baseTime == 0 {
		w.pcrCalculator.SetBaseTime(timestampUs)
	}

	pcrBase, pcrExt := w.pcrCalculator.CalculatePCR(timestampUs)

	// Adaptation field 길이 업데이트 (PCR 필드 6바이트 추가)
	adaptationLength := 7 // flags(1) + PCR(6)
	packet[4] = uint8(adaptationLength)

	// Adaptation field flags
	packet[5] = 0x10 // PCR_flag = 1

	// PCR 작성 (6바이트)
	// PCR_base (33비트) + reserved (6비트) + PCR_extension (9비트)
	pcrBytes := make([]byte, 6)

	// PCR_base 33비트를 6바이트에 배치
	pcrBytes[0] = uint8((pcrBase >> 25) & 0xFF)
	pcrBytes[1] = uint8((pcrBase >> 17) & 0xFF)
	pcrBytes[2] = uint8((pcrBase >> 9) & 0xFF)
	pcrBytes[3] = uint8((pcrBase >> 1) & 0xFF)
	pcrBytes[4] = uint8(((pcrBase & 0x01) << 7) | 0x7E | ((uint64(pcrExt) >> 8) & 0x01))
	pcrBytes[5] = uint8(pcrExt & 0xFF)

	copy(packet[6:12], pcrBytes)

	return 12 // 헤더 + adaptation field + PCR
}

// getContinuityCounter PID별 연속성 카운터 증가 및 반환
func (w *TSWriter) getContinuityCounter(pid uint16) uint8 {
	counter := w.continuityCounters[pid]
	w.continuityCounters[pid] = (counter + 1) & 0x0F
	return counter
}

// mergeFrameData 프레임 데이터 청크들을 하나의 바이트 슬라이스로 병합
func (w *TSWriter) mergeFrameData(chunks [][]byte) []byte {
	totalSize := 0
	for _, chunk := range chunks {
		totalSize += len(chunk)
	}

	result := make([]byte, totalSize)
	offset := 0
	for _, chunk := range chunks {
		copy(result[offset:], chunk)
		offset += len(chunk)
	}

	return result
}

// padWithNullPackets null 패킷으로 패딩하여 세그먼트 크기 조정
func (w *TSWriter) padWithNullPackets() {
	// 최소 세그먼트 크기 보장을 위해 null 패킷 추가
	minPackets := 100 // 최소 100개 패킷 (18.8KB)
	currentPackets := w.output.Len() / TSPacketSize

	for currentPackets < minPackets {
		nullPacket := make([]byte, TSPacketSize)
		nullPacket[0] = TSSyncByte
		nullPacket[1] = 0x1F // PID 상위 5비트
		nullPacket[2] = 0xFF // PID 하위 8비트 (0x1FFF)
		nullPacket[3] = 0x10 // No adaptation field, payload only

		// 나머지는 0xFF로 패딩
		for i := 4; i < TSPacketSize; i++ {
			nullPacket[i] = 0xFF
		}

		w.output.Write(nullPacket)
		currentPackets++
	}
}
