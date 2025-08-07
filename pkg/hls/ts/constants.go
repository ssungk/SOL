package ts

// MPEG-TS 관련 상수들

const (
	// TS 패킷 기본 정보
	TSPacketSize    = 188        // TS 패킷 크기 (바이트)
	TSSyncByte      = 0x47       // TS 동기 바이트
	TSHeaderMinSize = 4          // 최소 헤더 크기
	
	// PID (Packet Identifier) 값들
	PIDPatTable     = 0x0000     // PAT (Program Association Table)
	PIDCondAccess   = 0x0001     // Conditional Access Table
	PIDTSDesc       = 0x0002     // Transport Stream Description Table
	PIDNull         = 0x1FFF     // Null 패킷
	
	// 일반적인 PID 범위
	PIDPMTStart     = 0x0020     // PMT 시작 PID
	PIDVideoPrimary = 0x0100     // 기본 비디오 PID
	PIDAudioPrimary = 0x0101     // 기본 오디오 PID
	
	// Stream Type (PMT에서 사용)
	StreamTypeVideoH264  = 0x1B  // H.264/AVC 비디오
	StreamTypeAudioAAC   = 0x0F  // MPEG-4 AAC 오디오
	StreamTypeAudioMP3   = 0x03  // MPEG-1 Layer III 오디오
	StreamTypeVideoMPEG2 = 0x02  // MPEG-2 비디오
	
	// Adaptation Field Control
	AdaptationFieldNone         = 0x01 // 페이로드만
	AdaptationFieldOnly         = 0x02 // adaptation field만
	AdaptationFieldWithPayload  = 0x03 // 둘 다
	AdaptationFieldReserved     = 0x00 // 예약됨
	
	// PCR 관련 상수
	PCRFrequency = 90000         // PCR 주파수 (90kHz)
	PCRInterval  = 100           // PCR 간격 (ms)
	
	// PES 관련 상수
	PESStartCodePrefix = 0x000001 // PES 시작 코드 접두사
	PESVideoStreamID   = 0xE0     // 비디오 스트림 ID (0xE0-0xEF)
	PESAudioStreamID   = 0xC0     // 오디오 스트림 ID (0xC0-0xDF)
	
	// Table ID 값들
	TableIDPAT = 0x00            // PAT 테이블 ID
	TableIDPMT = 0x02            // PMT 테이블 ID
)

// TSPacketHeader TS 패킷 헤더 구조
type TSPacketHeader struct {
	SyncByte                 uint8  // 동기 바이트 (0x47)
	TransportErrorIndicator  bool   // 전송 오류 표시
	PayloadUnitStartIndicator bool  // 페이로드 단위 시작 표시
	TransportPriority        bool   // 전송 우선순위
	PID                      uint16 // 패킷 식별자
	TransportScramblingControl uint8 // 전송 스크램블링 제어
	AdaptationFieldControl   uint8  // adaptation field 제어
	ContinuityCounter        uint8  // 연속성 카운터
}

// AdaptationField adaptation field 구조
type AdaptationField struct {
	Length                    uint8  // adaptation field 길이
	DiscontinuityIndicator    bool   // 불연속성 표시
	RandomAccessIndicator     bool   // 랜덤 액세스 표시
	ElementaryStreamPriority  bool   // 기본 스트림 우선순위
	PCRFlag                   bool   // PCR 플래그
	OPCRFlag                  bool   // OPCR 플래그
	SplicingPointFlag         bool   // 스플라이싱 포인트 플래그
	TransportPrivateDataFlag  bool   // 전송 개인 데이터 플래그
	AdaptationFieldExtFlag    bool   // adaptation field 확장 플래그
	
	// PCR 필드 (PCRFlag가 true일 때)
	PCRBase      uint64 // PCR 기본값 (33비트)
	PCRExtension uint16 // PCR 확장값 (9비트)
}

// PESHeader PES 패킷 헤더 구조
type PESHeader struct {
	PacketStartCodePrefix uint32 // 패킷 시작 코드 접두사 (0x000001)
	StreamID              uint8  // 스트림 ID
	PacketLength          uint16 // 패킷 길이
	
	// Optional PES header fields
	MarkerBits            uint8  // 마커 비트 (10b)
	ScramblingControl     uint8  // 스크램블링 제어
	Priority              bool   // 우선순위
	DataAlignmentIndicator bool  // 데이터 정렬 표시
	Copyright             bool   // 저작권
	OriginalOrCopy        bool   // 원본 또는 복사본
	PTSDTSFlags           uint8  // PTS/DTS 플래그
	ESCRFlag              bool   // ESCR 플래그
	ESRateFlag            bool   // ES 비율 플래그
	DSMTrickModeFlag      bool   // DSM 트릭 모드 플래그
	AdditionalCopyInfoFlag bool  // 추가 복사 정보 플래그
	CRCFlag               bool   // CRC 플래그
	ExtensionFlag         bool   // 확장 플래그
	HeaderDataLength      uint8  // 헤더 데이터 길이
	
	// 타임스탬프
	PTS uint64 // Presentation Time Stamp
	DTS uint64 // Decode Time Stamp
}

// PSIHeader PSI (Program Specific Information) 헤더 구조
type PSIHeader struct {
	TableID                uint8  // 테이블 ID
	SectionSyntaxIndicator bool   // 섹션 구문 표시
	PrivateBit             bool   // 개인 비트
	Reserved1              uint8  // 예약됨 (2비트)
	SectionLength          uint16 // 섹션 길이
	TableIDExtension       uint16 // 테이블 ID 확장
	Reserved2              uint8  // 예약됨 (2비트)
	VersionNumber          uint8  // 버전 번호
	CurrentNextIndicator   bool   // 현재/다음 표시
	SectionNumber          uint8  // 섹션 번호
	LastSectionNumber      uint8  // 마지막 섹션 번호
}

// PMTElementaryStream PMT 기본 스트림 구조
type PMTElementaryStream struct {
	StreamType    uint8  // 스트림 타입
	ElementaryPID uint16 // 기본 PID
	ESInfoLength  uint16 // ES 정보 길이
	ESInfo        []byte // ES 정보 (descriptors)
}

// PCRCalculator PCR 계산 도구
type PCRCalculator struct {
	baseTime  int64 // 기준 시간 (마이크로초)
	clockRate int64 // 클록 비율 (90kHz = 90000)
}

// NewPCRCalculator PCR 계산기 생성
func NewPCRCalculator() *PCRCalculator {
	return &PCRCalculator{
		clockRate: PCRFrequency,
	}
}

// SetBaseTime 기준 시간 설정 (마이크로초)
func (calc *PCRCalculator) SetBaseTime(timeUs int64) {
	calc.baseTime = timeUs
}

// CalculatePCR 주어진 시간에 대한 PCR 계산
func (calc *PCRCalculator) CalculatePCR(timeUs int64) (uint64, uint16) {
	// 경과 시간 계산 (마이크로초)
	elapsedUs := timeUs - calc.baseTime
	
	// PCR 기본값 계산 (90kHz 기준)
	pcrBase := uint64((elapsedUs * calc.clockRate) / 1000000)
	
	// PCR 확장값 (27MHz 기준의 나머지)
	pcrExt := uint16(((elapsedUs * 27000000) / 1000000) % 300)
	
	return pcrBase & 0x1FFFFFFFF, pcrExt & 0x1FF // 33비트, 9비트 마스킹
}

// CalculatePTS PTS/DTS 계산 (90kHz 기준)
func CalculatePTS(timestampMs uint32) uint64 {
	return uint64(timestampMs) * 90 // 90kHz로 변환
}