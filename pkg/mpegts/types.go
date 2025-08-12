package mpegts

// Transport Stream 패킷 크기 상수
const (
	TSPacketSize = 188 // MPEG-TS 패킷 크기 (바이트)
)

// Transport Stream 헤더 관련 상수
const (
	// Sync byte
	SyncByte = 0x47

	// PID 관련
	PIDNull = 0x1FFF // Null 패킷 PID
	PIDPAT  = 0x0000 // PAT PID
	
	// 테이블 ID
	TableIDPAT = 0x00 // Program Association Table
	TableIDPMT = 0x02 // Program Map Table
	TableIDSDT = 0x42 // Service Description Table
)

// StreamType MPEG-TS에서 정의된 스트림 타입
type StreamType uint8

const (
	StreamTypeReserved           StreamType = 0x00
	StreamTypeMPEG1Video         StreamType = 0x01
	StreamTypeMPEG2Video         StreamType = 0x02
	StreamTypeMPEG1Audio         StreamType = 0x03
	StreamTypeMPEG2Audio         StreamType = 0x04
	StreamTypePrivateSection     StreamType = 0x05
	StreamTypePrivatePES         StreamType = 0x06
	StreamTypeMHEG               StreamType = 0x07
	StreamTypeDSMCC              StreamType = 0x08
	StreamTypeATMMultiplex       StreamType = 0x09
	StreamTypeDSMCCMultiprotocol StreamType = 0x0A
	StreamTypeDSMCCU_NMessages   StreamType = 0x0B
	StreamTypeDSMCCStreamPackets StreamType = 0x0C
	StreamTypeDSMCCAuxiliary     StreamType = 0x0D
	StreamTypeADTS               StreamType = 0x0F // AAC ADTS
	StreamTypeMPEG4Visual        StreamType = 0x10
	StreamTypeAAC                StreamType = 0x11 // AAC LATM
	StreamTypeMPEG4Systems       StreamType = 0x12
	StreamTypeH264               StreamType = 0x1B // H.264/AVC
	StreamTypeH265               StreamType = 0x24 // H.265/HEVC
)

// NALUType H.264/H.265 NALU 타입
type NALUType uint8

// H.264 NALU 타입
const (
	H264NALUTypeSlice        NALUType = 1
	H264NALUTypeSliceA       NALUType = 2
	H264NALUTypeSliceB       NALUType = 3
	H264NALUTypeSliceC       NALUType = 4
	H264NALUTypeSliceIDR     NALUType = 5
	H264NALUTypeSEI          NALUType = 6
	H264NALUTypeSPS          NALUType = 7
	H264NALUTypePPS          NALUType = 8
	H264NALUTypeAUD          NALUType = 9
	H264NALUTypeEndOfSeq     NALUType = 10
	H264NALUTypeEndOfStream  NALUType = 11
	H264NALUTypeFillerData   NALUType = 12
)

// H.265 NALU 타입
const (
	H265NALUTypeSliceTrail_N   NALUType = 0
	H265NALUTypeSliceTrail_R   NALUType = 1
	H265NALUTypeSliceTSA_N     NALUType = 2
	H265NALUTypeSliceTSA_R     NALUType = 3
	H265NALUTypeSliceSTSA_N    NALUType = 4
	H265NALUTypeSliceSTSA_R    NALUType = 5
	H265NALUTypeSliceRADL_N    NALUType = 6
	H265NALUTypeSliceRADL_R    NALUType = 7
	H265NALUTypeSliceRASL_N    NALUType = 8
	H265NALUTypeSliceRASL_R    NALUType = 9
	H265NALUTypeSliceBLA_W_LP  NALUType = 16
	H265NALUTypeSliceBLA_W_RADL NALUType = 17
	H265NALUTypeSliceBLA_N_LP  NALUType = 18
	H265NALUTypeSliceIDR_W_RADL NALUType = 19
	H265NALUTypeSliceIDR_N_LP  NALUType = 20
	H265NALUTypeSliceCRA_NUT   NALUType = 21
	H265NALUTypeVPS            NALUType = 32
	H265NALUTypeSPS            NALUType = 33
	H265NALUTypePPS            NALUType = 34
	H265NALUTypeAUD            NALUType = 35
	H265NALUTypeEOS            NALUType = 36
	H265NALUTypeEOB            NALUType = 37
	H265NALUTypeFD             NALUType = 38
	H265NALUTypeSEI_PREFIX     NALUType = 39
	H265NALUTypeSEI_SUFFIX     NALUType = 40
)

// TSPacketHeader Transport Stream 패킷 헤더
type TSPacketHeader struct {
	SyncByte                   uint8  // 동기 바이트 (0x47)
	TransportErrorIndicator    bool   // 전송 에러 지시자
	PayloadUnitStartIndicator  bool   // 페이로드 단위 시작 지시자
	TransportPriority          bool   // 전송 우선순위
	PID                        uint16 // Packet Identifier
	TransportScramblingControl uint8  // 스크램블링 제어
	AdaptationFieldControl     uint8  // 적응 필드 제어
	ContinuityCounter          uint8  // 연속성 카운터
}

// TSPacket Transport Stream 패킷
type TSPacket struct {
	Header          TSPacketHeader
	AdaptationField *AdaptationField // 적응 필드 (선택적)
	Payload         []byte           // 페이로드
}

// AdaptationField 적응 필드
type AdaptationField struct {
	Length                    uint8  // 적응 필드 길이
	DiscontinuityIndicator    bool   // 불연속 지시자
	RandomAccessIndicator     bool   // 랜덤 액세스 지시자
	ElementaryStreamPriority  bool   // 엘리멘터리 스트림 우선순위
	PCRFlag                   bool   // PCR 플래그
	OPCRFlag                  bool   // OPCR 플래그
	SplicingPointFlag         bool   // 스플라이싱 포인트 플래그
	TransportPrivateDataFlag  bool   // 전송 개인 데이터 플래그
	AdaptationFieldExtension  bool   // 적응 필드 확장
	PCR                       uint64 // Program Clock Reference (PCR이 있을 때)
	OPCR                      uint64 // Original Program Clock Reference (OPCR이 있을 때)
	SpliceCountdown           int8   // 스플라이스 카운트다운
	PrivateData               []byte // 개인 데이터
}

// PESPacketHeader PES 패킷 헤더
type PESPacketHeader struct {
	StreamID                  uint8  // 스트림 ID
	PacketLength              uint16 // 패킷 길이
	MarkerBits                uint8  // 마커 비트 (0b10)
	ScramblingControl         uint8  // 스크램블링 제어
	Priority                  bool   // 우선순위
	DataAlignmentIndicator    bool   // 데이터 정렬 지시자
	Copyright                 bool   // 저작권
	OriginalOrCopy            bool   // 원본 또는 복사본
	PTSDTSFlags               uint8  // PTS/DTS 플래그
	ESCRFlag                  bool   // ESCR 플래그
	ESRateFlag                bool   // ES 속도 플래그
	DSMTrickModeFlag          bool   // DSM 트릭 모드 플래그
	AdditionalCopyInfoFlag    bool   // 추가 복사 정보 플래그
	CRCFlag                   bool   // CRC 플래그
	ExtensionFlag             bool   // 확장 플래그
	HeaderDataLength          uint8  // 헤더 데이터 길이
	PTS                       uint64 // Presentation Time Stamp
	DTS                       uint64 // Decode Time Stamp
}

// PESPacket PES 패킷
type PESPacket struct {
	Header      PESPacketHeader
	Data        []byte // 엘리멘터리 스트림 데이터
	StartOffset int    // 데이터 시작 오프셋
}

// PATEntry PAT 테이블 엔트리
type PATEntry struct {
	ProgramNumber uint16 // 프로그램 번호
	PID           uint16 // PMT PID 또는 Network PID
}

// PAT Program Association Table
type PAT struct {
	TableID                uint8      // 테이블 ID
	SectionLength          uint16     // 섹션 길이
	TransportStreamID      uint16     // 전송 스트림 ID
	VersionNumber          uint8      // 버전 번호
	CurrentNextIndicator   bool       // 현재/다음 지시자
	SectionNumber          uint8      // 섹션 번호
	LastSectionNumber      uint8      // 마지막 섹션 번호
	Programs               []PATEntry // 프로그램 엔트리들
	CRC32                  uint32     // CRC32
}

// PMTStreamInfo PMT 스트림 정보
type PMTStreamInfo struct {
	StreamType    StreamType // 스트림 타입
	ElementaryPID uint16     // 엘리멘터리 스트림 PID
	ESInfoLength  uint16     // ES 정보 길이
	Descriptors   []byte     // 디스크립터
}

// PMT Program Map Table
type PMT struct {
	TableID              uint8           // 테이블 ID
	SectionLength        uint16          // 섹션 길이
	ProgramNumber        uint16          // 프로그램 번호
	VersionNumber        uint8           // 버전 번호
	CurrentNextIndicator bool            // 현재/다음 지시자
	SectionNumber        uint8           // 섹션 번호
	LastSectionNumber    uint8           // 마지막 섹션 번호
	PCR_PID              uint16          // PCR PID
	ProgramInfoLength    uint16          // 프로그램 정보 길이
	ProgramDescriptors   []byte          // 프로그램 디스크립터
	Streams              []PMTStreamInfo // 스트림 정보들
	CRC32                uint32          // CRC32
}

// NALU Network Abstraction Layer Unit (H.264/H.265)
type NALU struct {
	Type     NALUType // NALU 타입
	Data     []byte   // NALU 데이터 (Start Code 제외)
	IsConfig bool     // 설정 NALU 여부 (SPS/PPS/VPS)
	IsKey    bool     // 키프레임 NALU 여부
}

// CodecData 코덱 설정 데이터
type CodecData struct {
	Type     StreamType // 코덱 타입
	SPS      []byte     // Sequence Parameter Set (H.264/H.265)
	PPS      []byte     // Picture Parameter Set (H.264/H.265)
	VPS      []byte     // Video Parameter Set (H.265만)
	ASC      []byte     // Audio Specific Config (AAC)
	Profile  uint8      // 프로파일
	Level    uint8      // 레벨
	Width    uint16     // 비디오 너비
	Height   uint16     // 비디오 높이
	FPS      float64    // 프레임 레이트
	Channels uint8      // 오디오 채널 수
	SampleRate uint32   // 샘플링 레이트
}

// IsH264KeyFrame H.264 키프레임 여부 확인
func (nt NALUType) IsH264KeyFrame() bool {
	return nt == H264NALUTypeSliceIDR
}

// IsH264Config H.264 설정 NALU 여부 확인
func (nt NALUType) IsH264Config() bool {
	return nt == H264NALUTypeSPS || nt == H264NALUTypePPS
}

// IsH265KeyFrame H.265 키프레임 여부 확인
func (nt NALUType) IsH265KeyFrame() bool {
	return nt >= H265NALUTypeSliceBLA_W_LP && nt <= H265NALUTypeSliceCRA_NUT
}

// IsH265Config H.265 설정 NALU 여부 확인
func (nt NALUType) IsH265Config() bool {
	return nt == H265NALUTypeVPS || nt == H265NALUTypeSPS || nt == H265NALUTypePPS
}

// String StreamType을 문자열로 변환
func (st StreamType) String() string {
	switch st {
	case StreamTypeH264:
		return "H.264"
	case StreamTypeH265:
		return "H.265"
	case StreamTypeADTS:
		return "AAC ADTS"
	case StreamTypeAAC:
		return "AAC LATM"
	case StreamTypeMPEG1Video:
		return "MPEG-1 Video"
	case StreamTypeMPEG2Video:
		return "MPEG-2 Video"
	case StreamTypeMPEG1Audio:
		return "MPEG-1 Audio"
	case StreamTypeMPEG2Audio:
		return "MPEG-2 Audio"
	default:
		return "Unknown"
	}
}