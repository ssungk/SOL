package mpegts

import (
	"encoding/binary"
	"hash/crc32"
)

// CRC32 테이블 (MPEG-2에서 사용하는 다항식)
var crc32Table = crc32.MakeTable(0x04C11DB7)

// parsePAT PAT 테이블 파싱
func parsePAT(data []byte) (*PAT, error) {
	if len(data) < 8 {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: 0,
			Data:   data,
		}
	}

	pat := &PAT{}

	// 포인터 필드 처리 (첫 번째 바이트)
	pointerField := data[0]
	offset := 1 + int(pointerField) // 포인터 필드 + 포인터만큼 건너뛰기

	if offset >= len(data) {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: offset,
			Data:   data,
		}
	}

	// 테이블 헤더 파싱
	pat.TableID = data[offset]
	if pat.TableID != TableIDPAT {
		return nil, &ParseError{
			Err:    ErrInvalidTableID,
			Offset: offset,
			Data:   data,
		}
	}

	// 섹션 길이 (12비트)
	sectionLengthBytes := binary.BigEndian.Uint16(data[offset+1 : offset+3])
	pat.SectionLength = sectionLengthBytes & 0x0FFF

	if int(pat.SectionLength) > len(data)-offset-3 {
		return nil, &ParseError{
			Err:    ErrInvalidSectionLength,
			Offset: offset + 1,
			Data:   data,
		}
	}

	// 전송 스트림 ID
	pat.TransportStreamID = binary.BigEndian.Uint16(data[offset+3 : offset+5])

	// 버전 정보
	versionByte := data[offset+5]
	pat.VersionNumber = (versionByte & 0x3E) >> 1
	pat.CurrentNextIndicator = (versionByte & 0x01) != 0

	// 섹션 번호
	pat.SectionNumber = data[offset+6]
	pat.LastSectionNumber = data[offset+7]

	// 프로그램 엔트리 파싱
	entryOffset := offset + 8
	endOffset := offset + 3 + int(pat.SectionLength) - 4 // CRC32 제외

	for entryOffset < endOffset {
		if entryOffset+4 > endOffset {
			break // 불완전한 엔트리
		}

		entry := PATEntry{
			ProgramNumber: binary.BigEndian.Uint16(data[entryOffset : entryOffset+2]),
			PID:           binary.BigEndian.Uint16(data[entryOffset+2:entryOffset+4]) & 0x1FFF,
		}

		pat.Programs = append(pat.Programs, entry)
		entryOffset += 4
	}

	// CRC32 검증
	crcOffset := offset + 3 + int(pat.SectionLength) - 4
	if crcOffset+4 > len(data) {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: crcOffset,
			Data:   data,
		}
	}

	pat.CRC32 = binary.BigEndian.Uint32(data[crcOffset : crcOffset+4])
	
	// CRC 계산 및 검증
	crcData := data[offset+1 : crcOffset] // table_id 제외, CRC32 제외
	calculatedCRC := crc32.Checksum(crcData, crc32Table)
	if pat.CRC32 != calculatedCRC {
		return nil, &ParseError{
			Err:    ErrInvalidCRC,
			Offset: crcOffset,
			Data:   data,
		}
	}

	return pat, nil
}

// createPAT PAT 테이블 생성
func createPAT(transportStreamID uint16, programs []PATEntry) ([]byte, error) {
	// 섹션 길이 계산: 고정 헤더(5) + 프로그램 엔트리(4*개수) + CRC32(4)
	sectionLength := 5 + len(programs)*4 + 4
	totalLength := 1 + 3 + sectionLength // 포인터 필드(1) + 테이블 헤더(3) + 섹션 데이터

	data := make([]byte, totalLength)

	// 포인터 필드 (0으로 설정)
	data[0] = 0x00

	offset := 1

	// 테이블 헤더
	data[offset] = TableIDPAT                                    // table_id
	binary.BigEndian.PutUint16(data[offset+1:], 0xB000|uint16(sectionLength)) // section_syntax_indicator(1) + '0'(1) + reserved(2) + section_length(12)
	binary.BigEndian.PutUint16(data[offset+3:], transportStreamID)               // transport_stream_id

	// 버전 정보 (version_number=0, current_next_indicator=1)
	data[offset+5] = 0xC1 // reserved(2) + version_number(5) + current_next_indicator(1)

	// 섹션 번호
	data[offset+6] = 0x00 // section_number
	data[offset+7] = 0x00 // last_section_number

	// 프로그램 엔트리들
	entryOffset := offset + 8
	for _, program := range programs {
		binary.BigEndian.PutUint16(data[entryOffset:], program.ProgramNumber)
		binary.BigEndian.PutUint16(data[entryOffset+2:], 0xE000|program.PID) // reserved(3) + program_map_PID(13)
		entryOffset += 4
	}

	// CRC32 계산 및 추가
	crcData := data[offset+1 : entryOffset] // table_id 제외
	crc := crc32.Checksum(crcData, crc32Table)
	binary.BigEndian.PutUint32(data[entryOffset:], crc)

	return data, nil
}

// parsePMT PMT 테이블 파싱
func parsePMT(data []byte) (*PMT, error) {
	if len(data) < 12 {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: 0,
			Data:   data,
		}
	}

	pmt := &PMT{}

	// 포인터 필드 처리
	pointerField := data[0]
	offset := 1 + int(pointerField)

	if offset >= len(data) {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: offset,
			Data:   data,
		}
	}

	// 테이블 헤더 파싱
	pmt.TableID = data[offset]
	if pmt.TableID != TableIDPMT {
		return nil, &ParseError{
			Err:    ErrInvalidTableID,
			Offset: offset,
			Data:   data,
		}
	}

	// 섹션 길이
	sectionLengthBytes := binary.BigEndian.Uint16(data[offset+1 : offset+3])
	pmt.SectionLength = sectionLengthBytes & 0x0FFF

	if int(pmt.SectionLength) > len(data)-offset-3 {
		return nil, &ParseError{
			Err:    ErrInvalidSectionLength,
			Offset: offset + 1,
			Data:   data,
		}
	}

	// 프로그램 번호
	pmt.ProgramNumber = binary.BigEndian.Uint16(data[offset+3 : offset+5])

	// 버전 정보
	versionByte := data[offset+5]
	pmt.VersionNumber = (versionByte & 0x3E) >> 1
	pmt.CurrentNextIndicator = (versionByte & 0x01) != 0

	// 섹션 번호
	pmt.SectionNumber = data[offset+6]
	pmt.LastSectionNumber = data[offset+7]

	// PCR PID
	pmt.PCR_PID = binary.BigEndian.Uint16(data[offset+8:offset+10]) & 0x1FFF

	// 프로그램 정보 길이
	pmt.ProgramInfoLength = binary.BigEndian.Uint16(data[offset+10:offset+12]) & 0x0FFF

	// 프로그램 디스크립터
	descriptorOffset := offset + 12
	if pmt.ProgramInfoLength > 0 {
		if descriptorOffset+int(pmt.ProgramInfoLength) > len(data) {
			return nil, &ParseError{
				Err:    ErrBufferTooSmall,
				Offset: descriptorOffset,
				Data:   data,
			}
		}
		pmt.ProgramDescriptors = data[descriptorOffset : descriptorOffset+int(pmt.ProgramInfoLength)]
	}

	// 스트림 정보 파싱
	streamOffset := descriptorOffset + int(pmt.ProgramInfoLength)
	endOffset := offset + 3 + int(pmt.SectionLength) - 4 // CRC32 제외

	for streamOffset < endOffset {
		if streamOffset+5 > endOffset {
			break // 불완전한 스트림 정보
		}

		stream := PMTStreamInfo{
			StreamType:    StreamType(data[streamOffset]),
			ElementaryPID: binary.BigEndian.Uint16(data[streamOffset+1:streamOffset+3]) & 0x1FFF,
			ESInfoLength:  binary.BigEndian.Uint16(data[streamOffset+3:streamOffset+5]) & 0x0FFF,
		}

		streamOffset += 5

		// ES 디스크립터
		if stream.ESInfoLength > 0 {
			if streamOffset+int(stream.ESInfoLength) > endOffset {
				break
			}
			stream.Descriptors = data[streamOffset : streamOffset+int(stream.ESInfoLength)]
			streamOffset += int(stream.ESInfoLength)
		}

		pmt.Streams = append(pmt.Streams, stream)
	}

	// CRC32 검증
	crcOffset := offset + 3 + int(pmt.SectionLength) - 4
	if crcOffset+4 > len(data) {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: crcOffset,
			Data:   data,
		}
	}

	pmt.CRC32 = binary.BigEndian.Uint32(data[crcOffset : crcOffset+4])

	// CRC 계산 및 검증
	crcData := data[offset+1 : crcOffset]
	calculatedCRC := crc32.Checksum(crcData, crc32Table)
	if pmt.CRC32 != calculatedCRC {
		return nil, &ParseError{
			Err:    ErrInvalidCRC,
			Offset: crcOffset,
			Data:   data,
		}
	}

	return pmt, nil
}

// createPMT PMT 테이블 생성
func createPMT(programNumber uint16, pcrPID uint16, streams []PMTStreamInfo) ([]byte, error) {
	// 섹션 길이 계산
	programInfoLength := 0 // 프로그램 디스크립터는 생략
	streamInfoLength := 0
	for _, stream := range streams {
		streamInfoLength += 5 + len(stream.Descriptors)
	}
	
	sectionLength := 9 + programInfoLength + streamInfoLength + 4 // 고정헤더(9) + 프로그램정보 + 스트림정보 + CRC32(4)
	totalLength := 1 + 3 + sectionLength

	data := make([]byte, totalLength)

	// 포인터 필드
	data[0] = 0x00

	offset := 1

	// 테이블 헤더
	data[offset] = TableIDPMT
	binary.BigEndian.PutUint16(data[offset+1:], 0xB000|uint16(sectionLength))
	binary.BigEndian.PutUint16(data[offset+3:], programNumber)
	data[offset+5] = 0xC1 // version_number=0, current_next_indicator=1
	data[offset+6] = 0x00 // section_number
	data[offset+7] = 0x00 // last_section_number
	binary.BigEndian.PutUint16(data[offset+8:], 0xE000|pcrPID)          // PCR_PID
	binary.BigEndian.PutUint16(data[offset+10:], uint16(programInfoLength)) // program_info_length

	// 스트림 정보 작성
	streamOffset := offset + 12 + programInfoLength
	for _, stream := range streams {
		data[streamOffset] = uint8(stream.StreamType)
		binary.BigEndian.PutUint16(data[streamOffset+1:], 0xE000|stream.ElementaryPID)
		binary.BigEndian.PutUint16(data[streamOffset+3:], uint16(len(stream.Descriptors)))
		streamOffset += 5

		// 디스크립터 복사
		if len(stream.Descriptors) > 0 {
			copy(data[streamOffset:], stream.Descriptors)
			streamOffset += len(stream.Descriptors)
		}
	}

	// CRC32 계산 및 추가
	crcData := data[offset+1 : streamOffset]
	crc := crc32.Checksum(crcData, crc32Table)
	binary.BigEndian.PutUint32(data[streamOffset:], crc)

	return data, nil
}

// GetProgramPIDFromPAT PAT에서 특정 프로그램의 PMT PID 추출
func (pat *PAT) GetProgramPID(programNumber uint16) (uint16, bool) {
	for _, program := range pat.Programs {
		if program.ProgramNumber == programNumber {
			return program.PID, true
		}
	}
	return 0, false
}

// GetStreamPIDs PMT에서 모든 스트림 PID 추출
func (pmt *PMT) GetStreamPIDs() map[uint16]StreamType {
	streams := make(map[uint16]StreamType)
	for _, stream := range pmt.Streams {
		streams[stream.ElementaryPID] = stream.StreamType
	}
	return streams
}

// GetVideoStreams PMT에서 비디오 스트림만 추출
func (pmt *PMT) GetVideoStreams() []PMTStreamInfo {
	var videoStreams []PMTStreamInfo
	for _, stream := range pmt.Streams {
		if stream.StreamType == StreamTypeH264 || 
		   stream.StreamType == StreamTypeH265 || 
		   stream.StreamType == StreamTypeMPEG2Video ||
		   stream.StreamType == StreamTypeMPEG1Video {
			videoStreams = append(videoStreams, stream)
		}
	}
	return videoStreams
}

// GetAudioStreams PMT에서 오디오 스트림만 추출
func (pmt *PMT) GetAudioStreams() []PMTStreamInfo {
	var audioStreams []PMTStreamInfo
	for _, stream := range pmt.Streams {
		if stream.StreamType == StreamTypeADTS || 
		   stream.StreamType == StreamTypeAAC || 
		   stream.StreamType == StreamTypeMPEG2Audio ||
		   stream.StreamType == StreamTypeMPEG1Audio {
			audioStreams = append(audioStreams, stream)
		}
	}
	return audioStreams
}

// IsVideoStream 비디오 스트림 타입인지 확인
func (st StreamType) IsVideoStream() bool {
	return st == StreamTypeH264 || 
		   st == StreamTypeH265 || 
		   st == StreamTypeMPEG2Video ||
		   st == StreamTypeMPEG1Video ||
		   st == StreamTypeMPEG4Visual
}

// IsAudioStream 오디오 스트림 타입인지 확인
func (st StreamType) IsAudioStream() bool {
	return st == StreamTypeADTS || 
		   st == StreamTypeAAC || 
		   st == StreamTypeMPEG2Audio ||
		   st == StreamTypeMPEG1Audio
}