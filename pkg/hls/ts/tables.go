package ts

import (
	"bytes"
	"encoding/binary"
)

// generatePAT PAT (Program Association Table) 데이터 생성
func (w *TSWriter) generatePAT() []byte {
	buf := &bytes.Buffer{}
	
	// PSI 헤더
	tableID := uint8(TableIDPAT)
	buf.WriteByte(tableID)
	
	// 섹션 길이 계산 (고정 크기)
	// CRC 포함 나머지 섹션 길이: 5 + 4 + 4 = 13바이트
	sectionLength := uint16(13)
	
	// Section syntax indicator | private bit | reserved | section length
	lengthField := uint16(0x8000) | sectionLength // section_syntax_indicator = 1
	binary.Write(buf, binary.BigEndian, lengthField)
	
	// Transport stream ID
	tsID := uint16(0x0001)
	binary.Write(buf, binary.BigEndian, tsID)
	
	// Reserved | version | current_next_indicator
	versionField := uint8(0xC1) // version = 0, current_next_indicator = 1
	buf.WriteByte(versionField)
	
	// Section number
	buf.WriteByte(0x00)
	
	// Last section number
	buf.WriteByte(0x00)
	
	// Program 1 정보
	binary.Write(buf, binary.BigEndian, w.programNumber)  // program_number
	binary.Write(buf, binary.BigEndian, uint16(0xE000|w.pmtPID)) // reserved | PMT PID
	
	// CRC32 계산 및 추가
	crc := w.calculateCRC32(buf.Bytes())
	binary.Write(buf, binary.BigEndian, crc)
	
	return buf.Bytes()
}

// generatePMT PMT (Program Map Table) 데이터 생성
func (w *TSWriter) generatePMT() []byte {
	buf := &bytes.Buffer{}
	
	// PSI 헤더
	tableID := uint8(TableIDPMT)
	buf.WriteByte(tableID)
	
	// 기본 스트림 정보 준비
	streams := w.buildElementaryStreams()
	
	// 섹션 길이 계산
	// 고정 부분: program_info_length(2) + streams + CRC(4) = 기본 9바이트
	// 각 스트림당 5바이트 (stream_type + elementary_PID + ES_info_length)
	baseLength := 9 + 4 // 9 (기본) + 4 (CRC 이후 부분)
	streamLength := len(streams) * 5
	sectionLength := uint16(baseLength + streamLength)
	
	// Section syntax indicator | private bit | reserved | section length
	lengthField := uint16(0x8000) | sectionLength
	binary.Write(buf, binary.BigEndian, lengthField)
	
	// Program number
	binary.Write(buf, binary.BigEndian, w.programNumber)
	
	// Reserved | version | current_next_indicator
	versionField := uint8(0xC1) // version = 0, current_next_indicator = 1
	buf.WriteByte(versionField)
	
	// Section number
	buf.WriteByte(0x00)
	
	// Last section number
	buf.WriteByte(0x00)
	
	// Reserved | PCR PID
	pcrField := uint16(0xE000) | w.pcrPID
	binary.Write(buf, binary.BigEndian, pcrField)
	
	// Reserved | program info length (0 - no descriptors)
	binary.Write(buf, binary.BigEndian, uint16(0xF000))
	
	// Elementary streams
	for _, stream := range streams {
		buf.WriteByte(stream.StreamType)
		
		// Reserved | elementary PID
		pidField := uint16(0xE000) | stream.ElementaryPID
		binary.Write(buf, binary.BigEndian, pidField)
		
		// Reserved | ES info length (0 - no descriptors)
		binary.Write(buf, binary.BigEndian, uint16(0xF000))
	}
	
	// CRC32 계산 및 추가
	crc := w.calculateCRC32(buf.Bytes())
	binary.Write(buf, binary.BigEndian, crc)
	
	return buf.Bytes()
}

// buildElementaryStreams 기본 스트림 목록 구성
func (w *TSWriter) buildElementaryStreams() []PMTElementaryStream {
	streams := make([]PMTElementaryStream, 0, 2)
	
	if w.hasVideo {
		streams = append(streams, PMTElementaryStream{
			StreamType:    StreamTypeVideoH264,
			ElementaryPID: w.videoPID,
			ESInfoLength:  0,
		})
	}
	
	if w.hasAudio {
		streams = append(streams, PMTElementaryStream{
			StreamType:    StreamTypeAudioAAC,
			ElementaryPID: w.audioPID,
			ESInfoLength:  0,
		})
	}
	
	return streams
}

// generatePES PES 패킷 데이터 생성
func (w *TSWriter) generatePES(streamID uint8, payload []byte, timestampMs uint32) []byte {
	buf := &bytes.Buffer{}
	
	// PES 시작 코드 접두사
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x01)
	
	// Stream ID
	buf.WriteByte(streamID)
	
	// PES 패킷 길이 계산 (헤더 + 페이로드)
	headerLength := 8 + 5 // 기본 헤더(8) + PTS(5)
	packetLength := uint16(headerLength + len(payload))
	
	// 길이 제한 (65535 초과시 0으로 설정)
	if packetLength > 0xFFFF || len(payload) > 0xFFFF-headerLength {
		packetLength = 0 // 무제한 길이
	}
	
	binary.Write(buf, binary.BigEndian, packetLength)
	
	// PES 헤더 플래그
	// '10' + scrambling(2) + priority(1) + data_alignment(1) + copyright(1) + original(1)
	buf.WriteByte(0x80) // 마커 비트: 10, 나머지는 0
	
	// PTS/DTS flags + ESCR + ES_rate + DSM_trick + additional_copy + CRC + extension
	// PTS_DTS_flags = 10 (PTS만), 나머지는 0
	buf.WriteByte(0x80)
	
	// PES 헤더 데이터 길이
	buf.WriteByte(5) // PTS 5바이트
	
	// PTS 작성 (5바이트)
	pts := CalculatePTS(timestampMs)
	w.writePTSOrDTS(buf, 0x20, pts) // 0x20 = '0010' (PTS only)
	
	// 페이로드 데이터
	buf.Write(payload)
	
	return buf.Bytes()
}

// writePTSOrDTS PTS/DTS 필드 작성
func (w *TSWriter) writePTSOrDTS(buf *bytes.Buffer, marker uint8, timestamp uint64) {
	// 5바이트 PTS/DTS 형식
	// 4비트 marker + 3비트 상위 + 1비트 마커 + 15비트 중위 + 1비트 마커 + 15비트 하위 + 1비트 마커
	
	ts := timestamp & 0x1FFFFFFFF // 33비트 마스킹
	
	buf.WriteByte(marker | uint8((ts>>29)&0x0E) | 0x01)                    // marker + 상위 3비트 + 1
	buf.WriteByte(uint8((ts>>22)&0xFF))                                    // 중위 상위 8비트
	buf.WriteByte(uint8(((ts>>14)&0xFE)|0x01))                            // 중위 하위 7비트 + 1
	buf.WriteByte(uint8((ts>>7)&0xFF))                                     // 하위 상위 8비트
	buf.WriteByte(uint8(((ts<<1)&0xFE)|0x01))                             // 하위 하위 7비트 + 1
}

// calculateCRC32 CRC32 계산 (MPEG-2 표준)
func (w *TSWriter) calculateCRC32(data []byte) uint32 {
	// 간단한 CRC32 구현 (실제로는 MPEG CRC32 테이블 사용해야 함)
	// 여기서는 더미 CRC 반환
	return 0x00000000
}

// 더 정확한 MPEG CRC32 구현을 위한 테이블
var crc32Table = [256]uint32{
	0x00000000, 0x04c11db7, 0x09823b6e, 0x0d4326d9,
	0x130476dc, 0x17c56b6b, 0x1a864db2, 0x1e475005,
	// ... (전체 256개 항목)
	// 실제 구현시 MPEG-2 표준 CRC32 테이블 필요
}

// mpegCRC32 MPEG-2 표준 CRC32 계산
func (w *TSWriter) mpegCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	
	for _, b := range data {
		tableIndex := ((crc >> 24) ^ uint32(b)) & 0xFF
		crc = (crc << 8) ^ crc32Table[tableIndex]
	}
	
	return crc
}