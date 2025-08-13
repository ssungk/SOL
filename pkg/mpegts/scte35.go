package mpegts

import (
	"encoding/binary"
	"hash/crc32"
)

// SCTE-35 관련 상수
const (
	SCTE35StreamType = 0x86 // SCTE-35 스트림 타입
	SCTE35TableID    = 0xFC // SCTE-35 테이블 ID
)

// SCTE-35 명령 타입
const (
	SpliceNullCmd         = 0x00 // splice_null()
	SpliceScheduleCmd     = 0x04 // splice_schedule() 
	SpliceInsertCmd       = 0x05 // splice_insert()
	TimeSignalCmd         = 0x06 // time_signal()
	BandwidthReservCmd    = 0x07 // bandwidth_reservation()
	PrivateCommandCmd     = 0xFF // private_command()
)

// SpliceInfoSection SCTE-35 스플라이스 정보 섹션
type SpliceInfoSection struct {
	TableID                uint8  // 테이블 ID (0xFC)
	SectionSyntaxIndicator bool   // 섹션 문법 지시자 (false)
	PrivateIndicator       bool   // 개인 지시자 (false)  
	SectionLength          uint16 // 섹션 길이
	ProtocolVersion        uint8  // 프로토콜 버전
	EncryptedPacket        bool   // 암호화된 패킷
	EncryptionAlgorithm    uint8  // 암호화 알고리즘
	PTSAdjustment          uint64 // PTS 조정값 (33비트)
	CWIndex                uint8  // CW 인덱스
	Tier                   uint16 // Tier (12비트)
	SpliceCommandLength    uint16 // 스플라이스 명령 길이
	SpliceCommandType      uint8  // 스플라이스 명령 타입
	SpliceCommand          SpliceCommand // 스플라이스 명령
	DescriptorLoopLength   uint16 // 디스크립터 루프 길이
	Descriptors            []SpliceDescriptor // 디스크립터들
	CRC32                  uint32 // CRC32
}

// SpliceCommand 인터페이스
type SpliceCommand interface {
	CommandType() uint8
	Encode() []byte
}

// SpliceNull splice_null 명령
type SpliceNull struct{}

func (s *SpliceNull) CommandType() uint8 { return SpliceNullCmd }
func (s *SpliceNull) Encode() []byte     { return []byte{} }

// SpliceInsert splice_insert 명령
type SpliceInsert struct {
	SpliceEventID         uint32 // 스플라이스 이벤트 ID
	SpliceEventCancelInd  bool   // 스플라이스 이벤트 취소 지시자
	OutOfNetworkInd       bool   // 네트워크 외부 지시자
	ProgramSpliceFlag     bool   // 프로그램 스플라이스 플래그
	DurationFlag          bool   // 지속시간 플래그  
	SpliceImmediateFlag   bool   // 즉시 스플라이스 플래그
	SpliceTime            *SpliceTime // 스플라이스 시간
	BreakDuration         *BreakDuration // 중단 지속시간
	UniqueProgramID       uint16 // 고유 프로그램 ID
	AvailNum              uint8  // 사용 가능 번호
	AvailsExpected        uint8  // 예상 사용 가능 수
}

func (s *SpliceInsert) CommandType() uint8 { return SpliceInsertCmd }

func (s *SpliceInsert) Encode() []byte {
	var data []byte
	
	// splice_event_id (32비트)
	eventID := make([]byte, 4)
	binary.BigEndian.PutUint32(eventID, s.SpliceEventID)
	data = append(data, eventID...)
	
	// 플래그들 (8비트)
	flags := uint8(0)
	if s.SpliceEventCancelInd {
		flags |= 0x80
	}
	if !s.SpliceEventCancelInd {
		if s.OutOfNetworkInd {
			flags |= 0x40
		}
		if s.ProgramSpliceFlag {
			flags |= 0x20
		}
		if s.DurationFlag {
			flags |= 0x10
		}
		if s.SpliceImmediateFlag {
			flags |= 0x08
		}
	}
	data = append(data, flags)
	
	if !s.SpliceEventCancelInd {
		// splice_time (프로그램 스플라이스이고 즉시가 아닌 경우)
		if s.ProgramSpliceFlag && !s.SpliceImmediateFlag && s.SpliceTime != nil {
			data = append(data, s.SpliceTime.Encode()...)
		}
		
		// break_duration (지속시간이 있는 경우)
		if s.DurationFlag && s.BreakDuration != nil {
			data = append(data, s.BreakDuration.Encode()...)
		}
		
		// unique_program_id, avail_num, avails_expected
		uniqueID := make([]byte, 2)
		binary.BigEndian.PutUint16(uniqueID, s.UniqueProgramID)
		data = append(data, uniqueID...)
		data = append(data, s.AvailNum)
		data = append(data, s.AvailsExpected)
	}
	
	return data
}

// TimeSignal time_signal 명령
type TimeSignal struct {
	SpliceTime *SpliceTime // 스플라이스 시간
}

func (t *TimeSignal) CommandType() uint8 { return TimeSignalCmd }

func (t *TimeSignal) Encode() []byte {
	if t.SpliceTime != nil {
		return t.SpliceTime.Encode()
	}
	return []byte{0x00} // time_specified_flag = 0
}

// SpliceTime 스플라이스 시간
type SpliceTime struct {
	TimeSpecifiedFlag bool   // 시간 지정 플래그
	PTSTime           uint64 // PTS 시간 (33비트)
}

func (s *SpliceTime) Encode() []byte {
	data := make([]byte, 5)
	
	if s.TimeSpecifiedFlag {
		// time_specified_flag = 1, reserved = 0x3F (6비트), pts_time (33비트)
		data[0] = 0xFE | uint8((s.PTSTime>>32)&0x01)
		binary.BigEndian.PutUint32(data[1:], uint32(s.PTSTime))
	} else {
		// time_specified_flag = 0, reserved = 0x7F (7비트)
		data[0] = 0x7F
		// 나머지 바이트는 0으로 설정됨
	}
	
	return data
}

// BreakDuration 중단 지속시간
type BreakDuration struct {
	AutoReturn bool   // 자동 복귀 플래그
	Duration   uint64 // 지속시간 (33비트)
}

func (b *BreakDuration) Encode() []byte {
	data := make([]byte, 5)
	
	// auto_return (1비트) + reserved (6비트) + duration (33비트)
	if b.AutoReturn {
		data[0] = 0xFE | uint8((b.Duration>>32)&0x01)
	} else {
		data[0] = 0x7E | uint8((b.Duration>>32)&0x01)
	}
	binary.BigEndian.PutUint32(data[1:], uint32(b.Duration))
	
	return data
}

// SpliceDescriptor 스플라이스 디스크립터 인터페이스
type SpliceDescriptor interface {
	Tag() uint8
	Length() uint8
	Encode() []byte
}

// AvailDescriptor avail_descriptor (0x00)
type AvailDescriptor struct {
	ProviderAvailID uint32 // 제공자 사용 가능 ID
}

func (a *AvailDescriptor) Tag() uint8    { return 0x00 }
func (a *AvailDescriptor) Length() uint8 { return 4 }

func (a *AvailDescriptor) Encode() []byte {
	data := make([]byte, 6) // tag(1) + length(1) + data(4)
	data[0] = a.Tag()
	data[1] = a.Length()
	binary.BigEndian.PutUint32(data[2:], a.ProviderAvailID)
	return data
}

// DTMFDescriptor DTMF_descriptor (0x01)  
type DTMFDescriptor struct {
	Preroll uint8  // 프리롤
	DTMF    string // DTMF 문자들 (최대 8자)
}

func (d *DTMFDescriptor) Tag() uint8 { return 0x01 }

func (d *DTMFDescriptor) Length() uint8 {
	return uint8(1 + len(d.DTMF)) // preroll(1) + dtmf_chars
}

func (d *DTMFDescriptor) Encode() []byte {
	length := d.Length()
	data := make([]byte, 2+int(length))
	data[0] = d.Tag()
	data[1] = length
	data[2] = d.Preroll
	copy(data[3:], []byte(d.DTMF))
	return data
}

// SegmentationDescriptor segmentation_descriptor (0x02)
type SegmentationDescriptor struct {
	SegmentationEventID          uint32 // 세그멘테이션 이벤트 ID
	SegmentationEventCancelInd   bool   // 세그멘테이션 이벤트 취소 지시자
	ProgramSegmentationFlag      bool   // 프로그램 세그멘테이션 플래그
	SegmentationDurationFlag     bool   // 세그멘테이션 지속시간 플래그
	DeliveryNotRestrictedFlag    bool   // 전달 제한 없음 플래그
	SegmentationDuration         uint64 // 세그멘테이션 지속시간 (40비트)
	SegmentationTypeID           uint8  // 세그멘테이션 타입 ID
	SegmentNum                   uint8  // 세그먼트 번호
	SegmentsExpected             uint8  // 예상 세그먼트 수
	SubSegmentNum                uint8  // 서브세그먼트 번호 (선택적)
	SubSegmentsExpected          uint8  // 예상 서브세그먼트 수 (선택적)
}

func (s *SegmentationDescriptor) Tag() uint8 { return 0x02 }

func (s *SegmentationDescriptor) Length() uint8 {
	length := 11 // 기본 길이
	if s.SegmentationTypeID == 0x34 || s.SegmentationTypeID == 0x36 {
		length += 2 // sub_segment 필드들
	}
	return uint8(length)
}

func (s *SegmentationDescriptor) Encode() []byte {
	length := s.Length()
	data := make([]byte, 2+int(length))
	offset := 0
	
	data[offset] = s.Tag()
	offset++
	data[offset] = length
	offset++
	
	// segmentation_event_id
	binary.BigEndian.PutUint32(data[offset:], s.SegmentationEventID)
	offset += 4
	
	// 플래그들
	flags := uint8(0)
	if s.SegmentationEventCancelInd {
		flags |= 0x80
	}
	if !s.SegmentationEventCancelInd {
		if s.ProgramSegmentationFlag {
			flags |= 0x40
		}
		if s.SegmentationDurationFlag {
			flags |= 0x20
		}
		if s.DeliveryNotRestrictedFlag {
			flags |= 0x10
		}
	}
	data[offset] = flags
	offset++
	
	if !s.SegmentationEventCancelInd {
		if s.SegmentationDurationFlag {
			// segmentation_duration (40비트)
			data[offset] = uint8(s.SegmentationDuration >> 32)
			binary.BigEndian.PutUint32(data[offset+1:], uint32(s.SegmentationDuration))
			offset += 5
		}
		
		// segmentation_type_id, segment_num, segments_expected
		data[offset] = s.SegmentationTypeID
		data[offset+1] = s.SegmentNum  
		data[offset+2] = s.SegmentsExpected
		offset += 3
		
		// 서브세그먼트 정보 (특정 타입에만)
		if s.SegmentationTypeID == 0x34 || s.SegmentationTypeID == 0x36 {
			data[offset] = s.SubSegmentNum
			data[offset+1] = s.SubSegmentsExpected
		}
	}
	
	return data
}

// ParseSCTE35 SCTE-35 스플라이스 정보 섹션 파싱
func ParseSCTE35(data []byte) (*SpliceInfoSection, error) {
	if len(data) < 14 {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: 0,
			Data:   data,
		}
	}
	
	section := &SpliceInfoSection{}
	offset := 0
	
	// 테이블 ID
	section.TableID = data[offset]
	if section.TableID != SCTE35TableID {
		return nil, &ParseError{
			Err:    ErrInvalidTableID,
			Offset: offset,
			Data:   data,
		}
	}
	offset++
	
	// 섹션 문법 지시자, 개인 지시자, 섹션 길이
	flags := binary.BigEndian.Uint16(data[offset:])
	section.SectionSyntaxIndicator = (flags & 0x8000) != 0
	section.PrivateIndicator = (flags & 0x4000) != 0
	section.SectionLength = flags & 0x0FFF
	offset += 2
	
	if int(section.SectionLength) > len(data)-3 {
		return nil, &ParseError{
			Err:    ErrInvalidSectionLength,
			Offset: offset,
			Data:   data,
		}
	}
	
	// 프로토콜 버전
	section.ProtocolVersion = data[offset]
	offset++
	
	// 암호화 정보
	encFlags := data[offset]
	section.EncryptedPacket = (encFlags & 0x80) != 0
	section.EncryptionAlgorithm = (encFlags & 0x7E) >> 1
	offset++
	
	// PTS 조정 (33비트)
	section.PTSAdjustment = uint64(data[offset])<<25 | uint64(binary.BigEndian.Uint32(data[offset+1:]))>>7
	offset += 5
	
	// CW 인덱스
	section.CWIndex = data[offset]
	offset++
	
	// Tier (12비트)
	section.Tier = binary.BigEndian.Uint16(data[offset:]) & 0x0FFF
	offset += 2
	
	// 스플라이스 명령 길이
	section.SpliceCommandLength = binary.BigEndian.Uint16(data[offset:]) & 0x0FFF
	offset += 2
	
	// 스플라이스 명령 타입
	section.SpliceCommandType = data[offset]
	offset++
	
	// 스플라이스 명령 파싱
	if section.SpliceCommandLength > 0 {
		cmdData := data[offset : offset+int(section.SpliceCommandLength)-1] // command_type 제외
		switch section.SpliceCommandType {
		case SpliceInsertCmd:
			section.SpliceCommand = parseSpliceInsert(cmdData)
		case TimeSignalCmd:
			section.SpliceCommand = parseTimeSignal(cmdData)
		case SpliceNullCmd:
			section.SpliceCommand = &SpliceNull{}
		default:
			// 알 수 없는 명령은 건너뛰기
		}
		offset += int(section.SpliceCommandLength) - 1
	}
	
	// 디스크립터 루프 길이
	section.DescriptorLoopLength = binary.BigEndian.Uint16(data[offset:])
	offset += 2
	
	// 디스크립터 파싱
	descriptorEnd := offset + int(section.DescriptorLoopLength)
	for offset < descriptorEnd {
		if offset+2 > len(data) {
			break
		}
		
		tag := data[offset]
		length := data[offset+1]
		
		if offset+2+int(length) > len(data) {
			break
		}
		
		descData := data[offset+2 : offset+2+int(length)]
		
		switch tag {
		case 0x00: // avail_descriptor
			if len(descData) >= 4 {
				desc := &AvailDescriptor{
					ProviderAvailID: binary.BigEndian.Uint32(descData),
				}
				section.Descriptors = append(section.Descriptors, desc)
			}
		case 0x01: // DTMF_descriptor
			if len(descData) >= 1 {
				desc := &DTMFDescriptor{
					Preroll: descData[0],
					DTMF:    string(descData[1:]),
				}
				section.Descriptors = append(section.Descriptors, desc)
			}
		case 0x02: // segmentation_descriptor
			desc := parseSegmentationDescriptor(descData)
			if desc != nil {
				section.Descriptors = append(section.Descriptors, desc)
			}
		}
		
		offset += 2 + int(length)
	}
	
	// CRC32
	if offset+4 <= len(data) {
		section.CRC32 = binary.BigEndian.Uint32(data[offset:])
	}
	
	return section, nil
}

// parseSpliceInsert splice_insert 명령 파싱
func parseSpliceInsert(data []byte) *SpliceInsert {
	if len(data) < 5 {
		return nil
	}
	
	insert := &SpliceInsert{}
	offset := 0
	
	// splice_event_id
	insert.SpliceEventID = binary.BigEndian.Uint32(data[offset:])
	offset += 4
	
	// 플래그들
	flags := data[offset]
	insert.SpliceEventCancelInd = (flags & 0x80) != 0
	
	if !insert.SpliceEventCancelInd {
		insert.OutOfNetworkInd = (flags & 0x40) != 0
		insert.ProgramSpliceFlag = (flags & 0x20) != 0  
		insert.DurationFlag = (flags & 0x10) != 0
		insert.SpliceImmediateFlag = (flags & 0x08) != 0
	}
	offset++
	
	if !insert.SpliceEventCancelInd && len(data) > offset {
		// splice_time 파싱 (프로그램 스플라이스이고 즉시가 아닌 경우)
		if insert.ProgramSpliceFlag && !insert.SpliceImmediateFlag && len(data) >= offset+5 {
			insert.SpliceTime = parseSpliceTime(data[offset:])
			if insert.SpliceTime.TimeSpecifiedFlag {
				offset += 5
			} else {
				offset += 1
			}
		}
		
		// break_duration 파싱
		if insert.DurationFlag && len(data) >= offset+5 {
			insert.BreakDuration = parseBreakDuration(data[offset:])
			offset += 5
		}
		
		// unique_program_id, avail_num, avails_expected
		if len(data) >= offset+4 {
			insert.UniqueProgramID = binary.BigEndian.Uint16(data[offset:])
			insert.AvailNum = data[offset+2]
			insert.AvailsExpected = data[offset+3]
		}
	}
	
	return insert
}

// parseTimeSignal time_signal 명령 파싱
func parseTimeSignal(data []byte) *TimeSignal {
	signal := &TimeSignal{}
	
	if len(data) > 0 {
		signal.SpliceTime = parseSpliceTime(data)
	}
	
	return signal
}

// parseSpliceTime splice_time 파싱
func parseSpliceTime(data []byte) *SpliceTime {
	if len(data) < 1 {
		return nil
	}
	
	time := &SpliceTime{}
	time.TimeSpecifiedFlag = (data[0] & 0x80) != 0
	
	if time.TimeSpecifiedFlag && len(data) >= 5 {
		// PTS 시간 추출 (33비트)
		time.PTSTime = uint64(data[0]&0x01)<<32 | uint64(binary.BigEndian.Uint32(data[1:]))
	}
	
	return time
}

// parseBreakDuration break_duration 파싱
func parseBreakDuration(data []byte) *BreakDuration {
	if len(data) < 5 {
		return nil
	}
	
	duration := &BreakDuration{}
	duration.AutoReturn = (data[0] & 0x80) != 0
	duration.Duration = uint64(data[0]&0x01)<<32 | uint64(binary.BigEndian.Uint32(data[1:]))
	
	return duration
}

// parseSegmentationDescriptor segmentation_descriptor 파싱
func parseSegmentationDescriptor(data []byte) *SegmentationDescriptor {
	if len(data) < 6 {
		return nil
	}
	
	desc := &SegmentationDescriptor{}
	offset := 0
	
	// segmentation_event_id
	desc.SegmentationEventID = binary.BigEndian.Uint32(data[offset:])
	offset += 4
	
	// 플래그들
	flags := data[offset]
	desc.SegmentationEventCancelInd = (flags & 0x80) != 0
	
	if !desc.SegmentationEventCancelInd {
		desc.ProgramSegmentationFlag = (flags & 0x40) != 0
		desc.SegmentationDurationFlag = (flags & 0x20) != 0
		desc.DeliveryNotRestrictedFlag = (flags & 0x10) != 0
	}
	offset++
	
	if !desc.SegmentationEventCancelInd && len(data) > offset {
		// segmentation_duration (40비트)
		if desc.SegmentationDurationFlag && len(data) >= offset+5 {
			desc.SegmentationDuration = uint64(data[offset])<<32 | uint64(binary.BigEndian.Uint32(data[offset+1:]))
			offset += 5
		}
		
		// segmentation_type_id, segment_num, segments_expected
		if len(data) >= offset+3 {
			desc.SegmentationTypeID = data[offset]
			desc.SegmentNum = data[offset+1]
			desc.SegmentsExpected = data[offset+2]
			offset += 3
			
			// 서브세그먼트 정보
			if (desc.SegmentationTypeID == 0x34 || desc.SegmentationTypeID == 0x36) && len(data) >= offset+2 {
				desc.SubSegmentNum = data[offset]
				desc.SubSegmentsExpected = data[offset+1]
			}
		}
	}
	
	return desc
}

// CreateSCTE35 SCTE-35 스플라이스 정보 섹션 생성
func CreateSCTE35(section *SpliceInfoSection) ([]byte, error) {
	var data []byte
	
	// 테이블 ID
	data = append(data, SCTE35TableID)
	
	// 임시로 섹션 길이 공간 확보 (나중에 업데이트)
	lengthPos := len(data)
	data = append(data, 0x00, 0x00)
	
	// 프로토콜 버전
	data = append(data, section.ProtocolVersion)
	
	// 암호화 정보
	encFlags := uint8(0)
	if section.EncryptedPacket {
		encFlags |= 0x80
	}
	encFlags |= (section.EncryptionAlgorithm & 0x3F) << 1
	data = append(data, encFlags)
	
	// PTS 조정 (33비트를 40비트로 인코딩)
	ptsAdj := make([]byte, 5)
	ptsAdj[0] = uint8(section.PTSAdjustment >> 25)
	binary.BigEndian.PutUint32(ptsAdj[1:], uint32(section.PTSAdjustment<<7))
	data = append(data, ptsAdj...)
	
	// CW 인덱스
	data = append(data, section.CWIndex)
	
	// Tier (12비트를 16비트로 인코딩)
	tier := make([]byte, 2)
	binary.BigEndian.PutUint16(tier, 0xF000|section.Tier)
	data = append(data, tier...)
	
	// 스플라이스 명령
	var cmdData []byte
	if section.SpliceCommand != nil {
		cmdData = append([]byte{section.SpliceCommand.CommandType()}, section.SpliceCommand.Encode()...)
	}
	
	// 스플라이스 명령 길이
	cmdLength := make([]byte, 2)
	binary.BigEndian.PutUint16(cmdLength, 0xF000|uint16(len(cmdData)))
	data = append(data, cmdLength...)
	
	// 스플라이스 명령 데이터
	data = append(data, cmdData...)
	
	// 디스크립터들
	var descData []byte
	for _, desc := range section.Descriptors {
		descData = append(descData, desc.Encode()...)
	}
	
	// 디스크립터 루프 길이
	descLength := make([]byte, 2)
	binary.BigEndian.PutUint16(descLength, uint16(len(descData)))
	data = append(data, descLength...)
	
	// 디스크립터 데이터
	data = append(data, descData...)
	
	// 섹션 길이 업데이트 (CRC32 포함)
	sectionLength := len(data) - 3 + 4 // 헤더 3바이트 제외, CRC32 4바이트 포함
	binary.BigEndian.PutUint16(data[lengthPos:], uint16(sectionLength))
	
	// CRC32 계산 및 추가
	crcData := data[1:] // table_id 제외
	crc := crc32.Checksum(crcData, crc32Table)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)
	data = append(data, crcBytes...)
	
	return data, nil
}

// SCTE35ToPTS SCTE-35 시간을 PTS로 변환 (90kHz 기준)
func SCTE35ToPTS(scteTime uint64) uint64 {
	// SCTE-35 시간은 90kHz 클록 기준
	return scteTime
}

// PTSToSCTE35 PTS를 SCTE-35 시간으로 변환
func PTSToSCTE35(pts uint64) uint64 {
	return pts
}

// DurationToSCTE35 지속시간(초)을 SCTE-35 지속시간으로 변환
func DurationToSCTE35(seconds float64) uint64 {
	// 90kHz 클록 * 초
	return uint64(seconds * 90000)
}

// SCTE35ToDuration SCTE-35 지속시간을 초로 변환
func SCTE35ToDuration(scteTime uint64) float64 {
	return float64(scteTime) / 90000.0
}

// CreateAdBreakStart 광고 시작 신호 생성
func CreateAdBreakStart(eventID uint32, duration float64) *SpliceInfoSection {
	return &SpliceInfoSection{
		TableID:             SCTE35TableID,
		ProtocolVersion:     0,
		SpliceCommandType:   SpliceInsertCmd,
		SpliceCommand: &SpliceInsert{
			SpliceEventID:       eventID,
			OutOfNetworkInd:     true,
			ProgramSpliceFlag:   true,
			DurationFlag:        duration > 0,
			SpliceImmediateFlag: true,
			BreakDuration: &BreakDuration{
				AutoReturn: true,
				Duration:   DurationToSCTE35(duration),
			},
		},
	}
}

// CreateAdBreakEnd 광고 종료 신호 생성  
func CreateAdBreakEnd(eventID uint32) *SpliceInfoSection {
	return &SpliceInfoSection{
		TableID:           SCTE35TableID,
		ProtocolVersion:   0,
		SpliceCommandType: SpliceInsertCmd,
		SpliceCommand: &SpliceInsert{
			SpliceEventID:       eventID,
			OutOfNetworkInd:     false,
			ProgramSpliceFlag:   true,
			SpliceImmediateFlag: true,
		},
	}
}

// CreateTimeSignal 시간 신호 생성
func CreateTimeSignal(ptsTime uint64) *SpliceInfoSection {
	return &SpliceInfoSection{
		TableID:           SCTE35TableID,
		ProtocolVersion:   0,
		SpliceCommandType: TimeSignalCmd,
		SpliceCommand: &TimeSignal{
			SpliceTime: &SpliceTime{
				TimeSpecifiedFlag: true,
				PTSTime:           ptsTime,
			},
		},
	}
}

// IsAdBreakStart 광고 시작인지 확인
func (s *SpliceInfoSection) IsAdBreakStart() bool {
	if insert, ok := s.SpliceCommand.(*SpliceInsert); ok {
		return insert.OutOfNetworkInd && !insert.SpliceEventCancelInd
	}
	return false
}

// IsAdBreakEnd 광고 종료인지 확인
func (s *SpliceInfoSection) IsAdBreakEnd() bool {
	if insert, ok := s.SpliceCommand.(*SpliceInsert); ok {
		return !insert.OutOfNetworkInd && !insert.SpliceEventCancelInd
	}
	return false
}

// GetEventID 이벤트 ID 반환
func (s *SpliceInfoSection) GetEventID() uint32 {
	if insert, ok := s.SpliceCommand.(*SpliceInsert); ok {
		return insert.SpliceEventID
	}
	return 0
}

// GetDuration 광고 지속시간 반환 (초)
func (s *SpliceInfoSection) GetDuration() float64 {
	if insert, ok := s.SpliceCommand.(*SpliceInsert); ok {
		if insert.BreakDuration != nil {
			return SCTE35ToDuration(insert.BreakDuration.Duration)
		}
	}
	return 0
}

// GetSpliceTime 스플라이스 시간 반환
func (s *SpliceInfoSection) GetSpliceTime() (uint64, bool) {
	switch cmd := s.SpliceCommand.(type) {
	case *SpliceInsert:
		if cmd.SpliceTime != nil {
			return cmd.SpliceTime.PTSTime, cmd.SpliceTime.TimeSpecifiedFlag
		}
	case *TimeSignal:
		if cmd.SpliceTime != nil {
			return cmd.SpliceTime.PTSTime, cmd.SpliceTime.TimeSpecifiedFlag
		}
	}
	return 0, false
}

// GetPresentationTime 실제 프레젠테이션 시간 반환 (PTS 조정 적용)
func (s *SpliceInfoSection) GetPresentationTime() (uint64, bool) {
	if spliceTime, hasTime := s.GetSpliceTime(); hasTime {
		return spliceTime + s.PTSAdjustment, true
	}
	return 0, false
}