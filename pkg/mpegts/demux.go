package mpegts

import (
	"context"
	"log/slog"
	"sol/pkg/mpegts/codec"
	"sync"
)

// demuxer MPEGTS 디먹서 구현
type demuxer struct {
	ctx    context.Context
	config DemuxerConfig

	// 콜백 함수들
	frameCallback    FrameCallback
	metadataCallback MetadataCallback

	// 스트림 정보
	streams      map[uint16]StreamInfo // PID별 스트림 정보
	streamsMutex sync.RWMutex

	// PSI 테이블 정보
	pat          *PAT
	pmts         map[uint16]*PMT // 프로그램별 PMT
	psiMutex     sync.RWMutex

	// PES 버퍼 (PID별로 PES 패킷 재조립)
	pesBuffers   map[uint16][]byte // PID별 PES 데이터 버퍼
	pesBufferMutex sync.RWMutex

	// 연속성 검사
	continuityCounters map[uint16]uint8 // 예상되는 다음 연속성 카운터
	counterMutex       sync.RWMutex

	// 통계
	processedPackets uint64
	errorPackets     uint64
	statsMutex       sync.RWMutex
}

// newDemuxer 새 디먹서 생성
func newDemuxer(ctx context.Context, config DemuxerConfig) Demuxer {
	// 기본값 설정
	if config.BufferSize == 0 {
		config.BufferSize = 1024 * 1024 // 1MB
	}
	if config.MaxPIDCount == 0 {
		config.MaxPIDCount = 256
	}
	if config.PacketLossThreshold == 0 {
		config.PacketLossThreshold = 10
	}

	return &demuxer{
		ctx:                ctx,
		config:            config,
		streams:           make(map[uint16]StreamInfo),
		pmts:              make(map[uint16]*PMT),
		pesBuffers:        make(map[uint16][]byte),
		continuityCounters: make(map[uint16]uint8),
	}
}

// ProcessData 연속된 TS 패킷 데이터를 처리
func (d *demuxer) ProcessData(data []byte) error {
	// TS 패킷들을 순서대로 처리
	offset := 0
	for offset+TSPacketSize <= len(data) {
		packetData := data[offset : offset+TSPacketSize]
		
		if err := d.processPacket(packetData); err != nil {
			d.incrementErrorCount()
			if d.config.EnableDebugLog {
				slog.Debug("Failed to process TS packet", "err", err, "offset", offset)
			}
		}
		
		offset += TSPacketSize
	}

	return nil
}

// processPacket 단일 TS 패킷 처리
func (d *demuxer) processPacket(packetData []byte) error {
	// TS 패킷 파싱
	packet, err := parseTSPacket(packetData)
	if err != nil {
		return err
	}

	d.incrementProcessedCount()

	// 연속성 검사
	if err := d.checkContinuity(packet); err != nil {
		if d.config.EnableDebugLog {
			slog.Debug("Continuity error", "PID", packet.Header.PID, "err", err)
		}
	}

	// PID별로 처리
	switch packet.Header.PID {
	case PIDPAT:
		return d.processPAT(packet)
	case PIDNull:
		// Null 패킷은 무시
		return nil
	default:
		return d.processStream(packet)
	}
}

// processPAT PAT 패킷 처리
func (d *demuxer) processPAT(packet *TSPacket) error {
	if !packet.HasPayload() {
		return nil
	}

	pat, err := parsePAT(packet.Payload)
	if err != nil {
		return err
	}

	d.psiMutex.Lock()
	d.pat = pat
	d.psiMutex.Unlock()

	if d.config.EnableDebugLog {
		slog.Debug("PAT processed", "programs", len(pat.Programs))
	}

	return nil
}

// processPMT PMT 패킷 처리
func (d *demuxer) processPMT(packet *TSPacket) error {
	if !packet.HasPayload() {
		return nil
	}

	pmt, err := parsePMT(packet.Payload)
	if err != nil {
		return err
	}

	d.psiMutex.Lock()
	d.pmts[pmt.ProgramNumber] = pmt
	d.psiMutex.Unlock()

	// PMT에서 스트림 정보 업데이트
	d.streamsMutex.Lock()
	for _, streamInfo := range pmt.Streams {
		d.streams[streamInfo.ElementaryPID] = StreamInfo{
			PID:       streamInfo.ElementaryPID,
			Type:      streamInfo.StreamType,
			CodecData: nil, // 나중에 실제 데이터에서 추출
		}
	}
	d.streamsMutex.Unlock()

	if d.config.EnableDebugLog {
		slog.Debug("PMT processed", "program", pmt.ProgramNumber, "streams", len(pmt.Streams))
	}

	return nil
}

// processStream 스트림 패킷 처리
func (d *demuxer) processStream(packet *TSPacket) error {
	pid := packet.Header.PID

	// PMT PID 확인
	d.psiMutex.RLock()
	isPMT := false
	if d.pat != nil {
		for _, program := range d.pat.Programs {
			if program.PID == pid && program.ProgramNumber != 0 {
				isPMT = true
				break
			}
		}
	}
	d.psiMutex.RUnlock()

	if isPMT {
		return d.processPMT(packet)
	}

	// 일반 스트림 처리
	d.streamsMutex.RLock()
	streamInfo, exists := d.streams[pid]
	d.streamsMutex.RUnlock()

	if !exists {
		// 알 수 없는 PID는 무시
		return nil
	}

	return d.processPESPacket(packet, streamInfo)
}

// processPESPacket PES 패킷 처리
func (d *demuxer) processPESPacket(packet *TSPacket, streamInfo StreamInfo) error {
	if !packet.HasPayload() {
		return nil
	}

	pid := packet.Header.PID

	// PES 버퍼 관리
	d.pesBufferMutex.Lock()
	defer d.pesBufferMutex.Unlock()

	// 새로운 PES 패킷 시작
	if packet.Header.PayloadUnitStartIndicator {
		// 이전 PES 패킷 완료 처리
		if buffer, exists := d.pesBuffers[pid]; exists && len(buffer) > 0 {
			d.processPESBuffer(pid, buffer, streamInfo)
		}

		// 새 버퍼 시작
		d.pesBuffers[pid] = append([]byte(nil), packet.Payload...)
	} else {
		// 기존 PES 패킷에 데이터 추가
		if _, exists := d.pesBuffers[pid]; !exists {
			// 시작 패킷 없이 중간 패킷이 온 경우 무시
			return nil
		}
		d.pesBuffers[pid] = append(d.pesBuffers[pid], packet.Payload...)
	}

	return nil
}

// processPESBuffer 완성된 PES 버퍼 처리
func (d *demuxer) processPESBuffer(pid uint16, buffer []byte, streamInfo StreamInfo) {
	// PES 패킷 파싱
	pesPacket, err := parsePESPacket(buffer)
	if err != nil {
		if d.config.EnableDebugLog {
			slog.Debug("Failed to parse PES packet", "PID", pid, "err", err)
		}
		return
	}

	// 스트림 타입별로 처리
	switch streamInfo.Type {
	case StreamTypeH264:
		d.processH264Stream(pid, pesPacket, streamInfo)
	case StreamTypeH265:
		d.processH265Stream(pid, pesPacket, streamInfo)
	case StreamTypeADTS:
		d.processAACStream(pid, pesPacket, streamInfo)
	case StreamTypeSCTE35:
		d.processSCTE35Stream(pid, pesPacket, streamInfo)
	default:
		if d.config.EnableDebugLog {
			slog.Debug("Unsupported stream type", "PID", pid, "type", streamInfo.Type)
		}
	}
}

// processH264Stream H.264 스트림 처리
func (d *demuxer) processH264Stream(pid uint16, pesPacket *PESPacket, streamInfo StreamInfo) {
	// H.264 NALUs 파싱
	codecNALUs, err := codec.ParseH264NALUs(pesPacket.Data)
	if err != nil {
		if d.config.EnableDebugLog {
			slog.Debug("Failed to parse H.264 NALUs", "PID", pid, "err", err)
		}
		return
	}

	if len(codecNALUs) == 0 {
		return
	}

	// 타입 변환
	nalus := ConvertFromCodecNALUs(codecNALUs)

	// 코덱 데이터 업데이트 (SPS/PPS 발견시)
	codecData := ConvertFromCodecData(codec.ExtractH264Config(codecNALUs))
	d.updateCodecData(pid, codecData)

	// 미디어 프레임 생성
	timestamp := pesPacket.GetTimestampMs()
	frame := ConvertToMediaFrame(streamInfo.Type, nalus, timestamp)

	// 콜백 호출
	if d.frameCallback != nil {
		if err := d.frameCallback(pid, frame); err != nil && d.config.EnableDebugLog {
			slog.Debug("Frame callback error", "PID", pid, "err", err)
		}
	}
}

// processH265Stream H.265 스트림 처리
func (d *demuxer) processH265Stream(pid uint16, pesPacket *PESPacket, streamInfo StreamInfo) {
	// H.265 NALUs 파싱
	codecNALUs, err := codec.ParseH265NALUs(pesPacket.Data)
	if err != nil {
		if d.config.EnableDebugLog {
			slog.Debug("Failed to parse H.265 NALUs", "PID", pid, "err", err)
		}
		return
	}

	if len(codecNALUs) == 0 {
		return
	}

	// 타입 변환
	nalus := ConvertFromCodecNALUs(codecNALUs)

	// 코덱 데이터 업데이트 (VPS/SPS/PPS 발견시)
	codecData := ConvertFromCodecData(codec.ExtractH265Config(codecNALUs))
	d.updateCodecData(pid, codecData)

	// 미디어 프레임 생성
	timestamp := pesPacket.GetTimestampMs()
	frame := ConvertToMediaFrame(streamInfo.Type, nalus, timestamp)

	// 콜백 호출
	if d.frameCallback != nil {
		if err := d.frameCallback(pid, frame); err != nil && d.config.EnableDebugLog {
			slog.Debug("Frame callback error", "PID", pid, "err", err)
		}
	}
}

// processAACStream AAC 스트림 처리
func (d *demuxer) processAACStream(pid uint16, pesPacket *PESPacket, streamInfo StreamInfo) {
	// AAC 프레임들 파싱
	codecFrames, err := codec.ParseAACFrames(pesPacket.Data)
	if err != nil {
		if d.config.EnableDebugLog {
			slog.Debug("Failed to parse AAC frames", "PID", pid, "err", err)
		}
		return
	}

	timestamp := pesPacket.GetTimestampMs()

	// 각 AAC 프레임을 개별적으로 처리
	for _, codecFrame := range codecFrames {
		// 타입 변환
		frame := NALU{
			Type:     NALUType(codecFrame.Type),
			Data:     codecFrame.Data,
			IsConfig: codecFrame.IsConfig,
			IsKey:    codecFrame.IsKey,
		}
		
		mediaFrame := ConvertToMediaFrame(streamInfo.Type, []NALU{frame}, timestamp)

		// 콜백 호출
		if d.frameCallback != nil {
			if err := d.frameCallback(pid, mediaFrame); err != nil && d.config.EnableDebugLog {
				slog.Debug("Frame callback error", "PID", pid, "err", err)
			}
		}
	}
}

// processSCTE35Stream SCTE-35 스트림 처리
func (d *demuxer) processSCTE35Stream(pid uint16, pesPacket *PESPacket, streamInfo StreamInfo) {
	// SCTE-35는 PES가 아닌 섹션 형태로 전송됨
	// PES 패킷의 데이터 부분을 SCTE-35 섹션으로 파싱
	scte35Section, err := ParseSCTE35(pesPacket.Data)
	if err != nil {
		if d.config.EnableDebugLog {
			slog.Debug("Failed to parse SCTE-35 section", "PID", pid, "err", err)
		}
		return
	}

	if d.config.EnableDebugLog {
		slog.Debug("SCTE-35 message received", 
			"PID", pid, 
			"commandType", scte35Section.SpliceCommandType,
			"eventID", scte35Section.GetEventID(),
			"isAdStart", scte35Section.IsAdBreakStart(),
			"isAdEnd", scte35Section.IsAdBreakEnd())
	}

	// SCTE-35 메시지를 메타데이터로 변환
	if d.metadataCallback != nil {
		metadata := map[string]string{
			"type":        "scte35",
			"commandType": string(rune(scte35Section.SpliceCommandType)),
			"eventID":     string(rune(scte35Section.GetEventID())),
		}

		if scte35Section.IsAdBreakStart() {
			metadata["adBreak"] = "start"
			if duration := scte35Section.GetDuration(); duration > 0 {
				metadata["duration"] = string(rune(int(duration)))
			}
		} else if scte35Section.IsAdBreakEnd() {
			metadata["adBreak"] = "end"
		}

		if pts, hasTime := scte35Section.GetSpliceTime(); hasTime {
			metadata["spliceTime"] = string(rune(pts))
		}

		if presentationTime, hasTime := scte35Section.GetPresentationTime(); hasTime {
			metadata["presentationTime"] = string(rune(presentationTime))
		}

		if err := d.metadataCallback(pid, metadata); err != nil && d.config.EnableDebugLog {
			slog.Debug("SCTE-35 metadata callback error", "PID", pid, "err", err)
		}
	}
}

// updateCodecData 코덱 데이터 업데이트
func (d *demuxer) updateCodecData(pid uint16, codecData *CodecData) {
	if codecData == nil {
		return
	}

	d.streamsMutex.Lock()
	if streamInfo, exists := d.streams[pid]; exists {
		// 기존 데이터가 없거나 새로운 설정 정보가 있는 경우만 업데이트
		if streamInfo.CodecData == nil || 
		   len(codecData.SPS) > 0 || 
		   len(codecData.PPS) > 0 || 
		   len(codecData.VPS) > 0 || 
		   len(codecData.ASC) > 0 {
			streamInfo.CodecData = codecData
			d.streams[pid] = streamInfo

			// 메타데이터 콜백 호출
			if d.metadataCallback != nil {
				metadata := map[string]string{
					"codec":      streamInfo.Type.String(),
					"width":      string(rune(codecData.Width)),
					"height":     string(rune(codecData.Height)),
					"profile":    string(rune(codecData.Profile)),
					"level":      string(rune(codecData.Level)),
					"channels":   string(rune(codecData.Channels)),
					"sampleRate": string(rune(codecData.SampleRate)),
				}
				d.metadataCallback(pid, metadata)
			}
		}
	}
	d.streamsMutex.Unlock()
}

// checkContinuity 연속성 검사
func (d *demuxer) checkContinuity(packet *TSPacket) error {
	if packet.Header.PID == PIDNull {
		return nil // Null 패킷은 연속성 검사 안함
	}

	d.counterMutex.Lock()
	defer d.counterMutex.Unlock()

	expectedCC, exists := d.continuityCounters[packet.Header.PID]
	actualCC := packet.Header.ContinuityCounter

	if exists {
		if actualCC != expectedCC {
			// 연속성 에러 (패킷 손실 또는 중복)
			d.continuityCounters[packet.Header.PID] = (actualCC + 1) & 0x0F
			return &ParseError{
				Err:    ErrInvalidPacketSize, // 재사용
				Offset: 0,
				Data:   nil,
			}
		}
	}

	// 다음 예상 CC 업데이트
	d.continuityCounters[packet.Header.PID] = (actualCC + 1) & 0x0F
	return nil
}

// SetFrameCallback 프레임 처리 콜백 설정
func (d *demuxer) SetFrameCallback(callback FrameCallback) {
	d.frameCallback = callback
}

// SetMetadataCallback 메타데이터 처리 콜백 설정
func (d *demuxer) SetMetadataCallback(callback MetadataCallback) {
	d.metadataCallback = callback
}

// GetStreamInfo 스트림 정보 반환
func (d *demuxer) GetStreamInfo() map[uint16]StreamInfo {
	d.streamsMutex.RLock()
	defer d.streamsMutex.RUnlock()

	// 복사본 반환
	result := make(map[uint16]StreamInfo)
	for pid, info := range d.streams {
		result[pid] = info
	}
	return result
}

// Close 리소스 정리
func (d *demuxer) Close() error {
	// PES 버퍼들의 남은 데이터 처리
	d.pesBufferMutex.Lock()
	for pid, buffer := range d.pesBuffers {
		if len(buffer) > 0 {
			d.streamsMutex.RLock()
			if streamInfo, exists := d.streams[pid]; exists {
				d.processPESBuffer(pid, buffer, streamInfo)
			}
			d.streamsMutex.RUnlock()
		}
	}
	d.pesBuffers = make(map[uint16][]byte)
	d.pesBufferMutex.Unlock()

	return nil
}

// 통계 관련 헬퍼 함수들
func (d *demuxer) incrementProcessedCount() {
	d.statsMutex.Lock()
	d.processedPackets++
	d.statsMutex.Unlock()
}

func (d *demuxer) incrementErrorCount() {
	d.statsMutex.Lock()
	d.errorPackets++
	d.statsMutex.Unlock()
}

// GetStats 디먹서 통계 반환
func (d *demuxer) GetStats() (processed, errors uint64) {
	d.statsMutex.RLock()
	defer d.statsMutex.RUnlock()
	return d.processedPackets, d.errorPackets
}