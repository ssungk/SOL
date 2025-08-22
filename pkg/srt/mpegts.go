package srt

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"sol/pkg/media"
)

// MPEGTS 상수
const (
	MPEGTS_PACKET_SIZE = 188
	MPEGTS_SYNC_BYTE   = 0x47

	// PID 값
	PID_PAT = 0x0000 // Program Association Table
	PID_PMT = 0x1000 // Program Map Table (임시값)
)

// Stream Type 값 (PMT에서 사용)
const (
	STREAM_TYPE_VIDEO_H264 = 0x1B // H.264/AVC video
	STREAM_TYPE_AUDIO_AAC  = 0x0F // AAC audio
)

// MPEGTSParser MPEGTS 파서 구조체
type MPEGTSParser struct {
	session   *Session
	pmtPID    uint16
	videoPID  uint16
	audioPID  uint16
	streamSet bool
}

// NewMPEGTSParser 새로운 MPEGTS 파서 생성
func NewMPEGTSParser(session *Session) *MPEGTSParser {
	return &MPEGTSParser{
		session: session,
	}
}

// ParseMPEGTSData MPEGTS 데이터 파싱
func (p *MPEGTSParser) ParseMPEGTSData(data []byte) error {
	// 188바이트 패킷 단위로 처리
	for i := 0; i+MPEGTS_PACKET_SIZE <= len(data); i += MPEGTS_PACKET_SIZE {
		packet := data[i : i+MPEGTS_PACKET_SIZE]
		if err := p.parsePacket(packet); err != nil {
			slog.Debug("Failed to parse MPEGTS packet", "err", err)
		}
	}
	return nil
}

// parsePacket MPEGTS 패킷 파싱
func (p *MPEGTSParser) parsePacket(packet []byte) error {
	if len(packet) != MPEGTS_PACKET_SIZE {
		return fmt.Errorf("invalid packet size: %d", len(packet))
	}

	// Sync byte 확인
	if packet[0] != MPEGTS_SYNC_BYTE {
		return fmt.Errorf("invalid sync byte: 0x%02x", packet[0])
	}

	// Transport Error Indicator
	tei := (packet[1] & 0x80) != 0
	if tei {
		return fmt.Errorf("transport error indicator set")
	}

	// Payload Unit Start Indicator
	pusi := (packet[1] & 0x40) != 0

	// Transport Priority
	// transportPriority := (packet[1] & 0x20) != 0

	// PID
	pid := uint16(packet[1]&0x1F)<<8 | uint16(packet[2])

	// Transport Scrambling Control
	// tsc := (packet[3] & 0xC0) >> 6

	// Adaptation Field Control
	afc := (packet[3] & 0x30) >> 4

	// Continuity Counter
	// cc := packet[3] & 0x0F

	// 페이로드 시작 위치 계산
	payloadStart := 4

	// Adaptation Field 처리
	if afc == 2 || afc == 3 {
		if len(packet) <= payloadStart {
			return fmt.Errorf("packet too short for adaptation field")
		}
		adaptationLength := int(packet[payloadStart])
		payloadStart += 1 + adaptationLength
	}

	// 페이로드가 없는 경우
	if afc == 2 || payloadStart >= len(packet) {
		return nil
	}

	payload := packet[payloadStart:]

	// PID에 따른 처리
	switch pid {
	case PID_PAT:
		return p.parsePAT(payload, pusi)
	case p.pmtPID:
		return p.parsePMT(payload, pusi)
	case p.videoPID:
		return p.parseVideoES(payload, pusi)
	case p.audioPID:
		return p.parseAudioES(payload, pusi)
	default:
		// 알 수 없는 PID는 무시
		return nil
	}
}

// parsePAT Program Association Table 파싱
func (p *MPEGTSParser) parsePAT(payload []byte, pusi bool) error {
	if !pusi || len(payload) < 1 {
		return nil
	}

	// Pointer field 건너뛰기
	pointerField := payload[0]
	if len(payload) <= int(pointerField) {
		return fmt.Errorf("invalid pointer field in PAT")
	}

	patData := payload[1+pointerField:]
	if len(patData) < 8 {
		return fmt.Errorf("PAT too short")
	}

	// Section length
	sectionLength := binary.BigEndian.Uint16(patData[1:3]) & 0x0FFF
	if len(patData) < int(sectionLength)+3 {
		return fmt.Errorf("PAT section incomplete")
	}

	// Program 정보 파싱 (첫 번째 프로그램만 사용)
	if sectionLength >= 9 {
		// 첫 번째 프로그램의 PMT PID 추출
		p.pmtPID = (uint16(patData[10]&0x1F) << 8) | uint16(patData[11])
		slog.Debug("Found PMT PID in PAT", "pmtPID", p.pmtPID)
	}

	return nil
}

// parsePMT Program Map Table 파싱
func (p *MPEGTSParser) parsePMT(payload []byte, pusi bool) error {
	if !pusi || len(payload) < 1 {
		return nil
	}

	// Pointer field 건너뛰기
	pointerField := payload[0]
	if len(payload) <= int(pointerField) {
		return fmt.Errorf("invalid pointer field in PMT")
	}

	pmtData := payload[1+pointerField:]
	if len(pmtData) < 12 {
		return fmt.Errorf("PMT too short")
	}

	// Section length
	sectionLength := (uint16(pmtData[1]&0x0F) << 8) | uint16(pmtData[2])
	if len(pmtData) < int(sectionLength)+3 {
		return fmt.Errorf("PMT section incomplete")
	}

	// Program info length
	programInfoLength := (uint16(pmtData[10]&0x0F) << 8) | uint16(pmtData[11])
	
	// Elementary Stream 정보 시작 위치
	esStart := 12 + int(programInfoLength)
	esData := pmtData[esStart:]

	// Elementary Stream 정보 파싱
	for i := 0; i+4 < len(esData); {
		streamType := esData[i]
		elementaryPID := (uint16(esData[i+1]&0x1F) << 8) | uint16(esData[i+2])
		esInfoLength := (uint16(esData[i+3]&0x0F) << 8) | uint16(esData[i+4])

		switch streamType {
		case STREAM_TYPE_VIDEO_H264:
			p.videoPID = elementaryPID
			slog.Debug("Found H264 video stream", "pid", elementaryPID)
		case STREAM_TYPE_AUDIO_AAC:
			p.audioPID = elementaryPID
			slog.Debug("Found AAC audio stream", "pid", elementaryPID)
		}

		i += 5 + int(esInfoLength)
	}

	// 스트림 설정 (한 번만)
	if !p.streamSet && p.videoPID != 0 {
		p.setupStream()
		p.streamSet = true
	}

	return nil
}

// setupStream 스트림 설정
func (p *MPEGTSParser) setupStream() {
	if p.session.stream != nil {
		return
	}

	streamID := fmt.Sprintf("srt_%d", p.session.ID())
	p.session.stream = media.NewStream(streamID)
	p.session.streamID = streamID

	// 비디오 트랙 추가
	if p.videoPID != 0 {
		p.session.stream.AddTrack(media.H264, media.TimeScaleSRT)
		slog.Info("Added H264 video track", "streamId", streamID)
	}

	// 오디오 트랙 추가
	if p.audioPID != 0 {
		p.session.stream.AddTrack(media.AAC, media.TimeScaleSRT)
		slog.Info("Added AAC audio track", "streamId", streamID)
	}

	// MediaServer에 발행 시작 이벤트 전송
	if p.session.mediaServerChannel != nil {
		responseChan := make(chan media.Response, 1)
		select {
		case p.session.mediaServerChannel <- media.PublishStarted{
			ID:           p.session.ID(),
			Stream:       p.session.stream,
			ResponseChan: responseChan,
		}:
			// 응답 대기
			response := <-responseChan
			if response.Success {
				slog.Info("SRT stream publishing started", "streamId", streamID)
			} else {
				slog.Error("Failed to start SRT stream publishing", "err", response.Error)
			}
		default:
			slog.Warn("MediaServer channel full", "streamId", streamID)
		}
	}
}

// parseVideoES 비디오 Elementary Stream 파싱
func (p *MPEGTSParser) parseVideoES(payload []byte, pusi bool) error {
	if !pusi || len(payload) < 6 {
		return nil
	}

	// PES 헤더 파싱
	if payload[0] != 0x00 || payload[1] != 0x00 || payload[2] != 0x01 {
		return fmt.Errorf("invalid PES start code")
	}

	// PES 헤더 길이 계산
	pesHeaderLength := int(payload[8])
	if len(payload) <= 9+pesHeaderLength {
		return nil
	}

	// PTS 추출 (간단화)
	var pts uint64
	if (payload[7] & 0x80) != 0 { // PTS present
		if len(payload) >= 14 {
			pts = uint64(payload[9]&0x0E)<<29 |
				uint64(payload[10])<<22 |
				uint64(payload[11]&0xFE)<<14 |
				uint64(payload[12])<<7 |
				uint64(payload[13]&0xFE)>>1
		}
	}

	// Elementary Stream 데이터
	esData := payload[9+pesHeaderLength:]
	if len(esData) == 0 {
		return nil
	}

	// H.264 프레임 생성 및 전송
	frameType := media.TypeData
	if p.isKeyFrame(esData) {
		frameType = media.TypeKey
	}
	
	frame := media.NewFrame(
		0, // 비디오 트랙 인덱스
		media.H264,
		media.FormatH26xAnnexB,
		frameType,
		pts,
		0, // CTS (단순화)
		esData,
	)

	if p.session.stream != nil {
		return p.session.stream.SendFrame(frame)
	}

	return nil
}

// parseAudioES 오디오 Elementary Stream 파싱
func (p *MPEGTSParser) parseAudioES(payload []byte, pusi bool) error {
	if !pusi || len(payload) < 6 {
		return nil
	}

	// PES 헤더 파싱 (비디오와 동일)
	if payload[0] != 0x00 || payload[1] != 0x00 || payload[2] != 0x01 {
		return fmt.Errorf("invalid PES start code")
	}

	pesHeaderLength := int(payload[8])
	if len(payload) <= 9+pesHeaderLength {
		return nil
	}

	// PTS 추출
	var pts uint64
	if (payload[7] & 0x80) != 0 {
		if len(payload) >= 14 {
			pts = uint64(payload[9]&0x0E)<<29 |
				uint64(payload[10])<<22 |
				uint64(payload[11]&0xFE)<<14 |
				uint64(payload[12])<<7 |
				uint64(payload[13]&0xFE)>>1
		}
	}

	esData := payload[9+pesHeaderLength:]
	if len(esData) == 0 {
		return nil
	}

	// AAC 프레임 생성 및 전송
	frame := media.NewFrame(
		1, // 오디오 트랙 인덱스
		media.AAC,
		media.FormatAACRaw,
		media.TypeData, // 오디오는 모두 일반 데이터로 처리
		pts,
		0, // CTS (단순화)
		esData,
	)

	if p.session.stream != nil {
		return p.session.stream.SendFrame(frame)
	}

	return nil
}

// isKeyFrame H.264 키프레임 여부 확인
func (p *MPEGTSParser) isKeyFrame(data []byte) bool {
	// NAL unit 탐색
	for i := 0; i+4 < len(data); i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			if i+5 < len(data) {
				nalType := data[i+4] & 0x1F
				if nalType == 5 { // IDR frame
					return true
				}
			}
		} else if i+3 < len(data) && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			if i+4 < len(data) {
				nalType := data[i+3] & 0x1F
				if nalType == 5 { // IDR frame
					return true
				}
			}
		}
	}
	return false
}