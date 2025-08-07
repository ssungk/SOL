package fmp4

// MP4 박스 타입 상수들 (4바이트)
const (
	// 컨테이너 박스들
	BoxTypeFTYP = "ftyp" // File Type Box
	BoxTypeMOOV = "moov" // Movie Box
	BoxTypeMVHD = "mvhd" // Movie Header Box
	BoxTypeTRAK = "trak" // Track Box
	BoxTypeTKHD = "tkhd" // Track Header Box
	BoxTypeMDIA = "mdia" // Media Box
	BoxTypeMDHD = "mdhd" // Media Header Box
	BoxTypeHDLR = "hdlr" // Handler Reference Box
	BoxTypeMINF = "minf" // Media Information Box
	BoxTypeVMHD = "vmhd" // Video Media Header Box
	BoxTypeSMHD = "smhd" // Sound Media Header Box
	BoxTypeDINF = "dinf" // Data Information Box
	BoxTypeDREF = "dref" // Data Reference Box
	BoxTypeSTBL = "stbl" // Sample Table Box
	BoxTypeSTSD = "stsd" // Sample Description Box
	BoxTypeSTTS = "stts" // Sample Time to Sample Box
	BoxTypeSTSC = "stsc" // Sample to Chunk Box
	BoxTypeSTSZ = "stsz" // Sample Size Box
	BoxTypeSTCO = "stco" // Chunk Offset Box
	
	// Fragmented MP4 박스들
	BoxTypeMOOF = "moof" // Movie Fragment Box
	BoxTypeMFHD = "mfhd" // Movie Fragment Header Box
	BoxTypeTRAF = "traf" // Track Fragment Box
	BoxTypeTFHD = "tfhd" // Track Fragment Header Box
	BoxTypeTRUN = "trun" // Track Fragment Run Box
	BoxTypeMDAT = "mdat" // Media Data Box
	
	// 코덱별 샘플 엔트리
	BoxTypeAVC1 = "avc1" // AVC Video Sample Entry
	BoxTypeMP4A = "mp4a" // MP4 Audio Sample Entry
	BoxTypeAVCC = "avcC" // AVC Configuration Box
	BoxTypeESDS = "esds" // Elementary Stream Descriptor Box
	
	// 브랜드 타입들
	BrandISOBM = "isom" // ISO Base Media
	BrandMP41  = "mp41" // MP4 version 1
	BrandMP42  = "mp42" // MP4 version 2
	BrandAVC1  = "avc1" // AVC
	BrandDASH  = "dash" // DASH
	BrandMSE1  = "mse1" // Media Source Extensions
	BrandISO5  = "iso5" // ISO/IEC 14496-12:2005
	
	// 핸들러 타입
	HandlerVideo = "vide" // Video handler
	HandlerAudio = "soun" // Audio handler
	HandlerHint  = "hint" // Hint handler
	
	// 타임스케일 상수들
	MovieTimeScale = 1000     // Movie timescale (1000 = 1ms resolution)
	VideoTimeScale = 90000    // Video timescale (90kHz)
	AudioTimeScale = 48000    // Audio timescale (48kHz, 44.1kHz 등 샘플레이트와 일치)
	
	// 기본 값들
	DefaultTrackID = 1
	VideoTrackID   = 1
	AudioTrackID   = 2
	
	// 플래그들 (tfhd box)
	TFHDBaseDataOffset        = 0x000001
	TFHDSampleDescriptionIndex = 0x000002
	TFHDDefaultSampleDuration = 0x000008
	TFHDDefaultSampleSize     = 0x000010
	TFHDDefaultSampleFlags    = 0x000020
	
	// 플래그들 (trun box)
	TRUNDataOffset           = 0x000001
	TRUNFirstSampleFlags     = 0x000004
	TRUNSampleDuration       = 0x000100
	TRUNSampleSize           = 0x000200
	TRUNSampleFlags          = 0x000400
	TRUNSampleCompositionOffset = 0x000800
	
	// 샘플 플래그들
	SampleFlagKeyFrame     = 0x00000000 // 키프레임 (의존성 없음)
	SampleFlagNonKeyFrame  = 0x00010000 // 비키프레임 (의존성 있음)
	SampleFlagDisposable   = 0x00080000 // 삭제 가능한 프레임
)

// BoxHeader MP4 박스 헤더 구조
type BoxHeader struct {
	Size uint32 // 박스 크기 (8바이트 헤더 포함)
	Type string // 박스 타입 (4바이트)
}

// FullBoxHeader Full Box 헤더 구조 (version + flags 포함)
type FullBoxHeader struct {
	BoxHeader
	Version uint8  // 버전
	Flags   uint32 // 플래그 (24비트)
}

// SampleEntry 샘플 엔트리 기본 구조
type SampleEntry struct {
	Reserved   [6]uint8 // 예약됨
	DataRefIndex uint16 // 데이터 참조 인덱스
}

// VideoSampleEntry 비디오 샘플 엔트리
type VideoSampleEntry struct {
	SampleEntry
	PreDefined1    uint16 // 미리 정의됨
	Reserved1      uint16 // 예약됨
	PreDefined2    [3]uint32 // 미리 정의됨
	Width          uint16 // 너비
	Height         uint16 // 높이
	HorizResolution uint32 // 수평 해상도 (72 DPI = 0x00480000)
	VertResolution  uint32 // 수직 해상도 (72 DPI = 0x00480000)
	Reserved2       uint32 // 예약됨
	FrameCount      uint16 // 프레임 수 (1)
	CompressorName  [32]uint8 // 압축기 이름
	Depth           uint16 // 색상 깊이 (24)
	PreDefined3     int16  // 미리 정의됨 (-1)
}

// AudioSampleEntry 오디오 샘플 엔트리
type AudioSampleEntry struct {
	SampleEntry
	Reserved1      [2]uint32 // 예약됨
	ChannelCount   uint16    // 채널 수
	SampleSize     uint16    // 샘플 크기 (16비트)
	PreDefined     uint16    // 미리 정의됨
	Reserved2      uint16    // 예약됨
	SampleRate     uint32    // 샘플 레이트 (상위 16비트)
}

// AVCConfiguration AVC 설정 구조 (avcC box)
type AVCConfiguration struct {
	ConfigurationVersion uint8    // 설정 버전 (1)
	AVCProfileIndication uint8    // AVC 프로파일 표시
	ProfileCompatibility uint8    // 프로파일 호환성
	AVCLevelIndication   uint8    // AVC 레벨 표시
	LengthSizeMinusOne   uint8    // NALU 길이 크기 - 1
	NumSPS               uint8    // SPS 개수
	SPSData              [][]byte // SPS 데이터
	NumPPS               uint8    // PPS 개수  
	PPSData              [][]byte // PPS 데이터
}

// TrackFragment 트랙 프래그먼트 정보
type TrackFragment struct {
	TrackID           uint32          // 트랙 ID
	BaseDataOffset    uint64          // 기본 데이터 오프셋
	SampleDescription uint32          // 샘플 설명 인덱스
	DefaultDuration   uint32          // 기본 샘플 지속시간
	DefaultSize       uint32          // 기본 샘플 크기
	DefaultFlags      uint32          // 기본 샘플 플래그
	Samples           []SampleInfo    // 샘플 정보 목록
}

// SampleInfo 샘플 정보
type SampleInfo struct {
	Duration          uint32 // 지속시간
	Size             uint32 // 크기
	Flags            uint32 // 플래그
	CompositionOffset int32  // 구성 오프셋 (PTS - DTS)
	Data             []byte // 실제 데이터
}

// Fragment 프래그먼트 구조
type Fragment struct {
	SequenceNumber  uint32           // 시퀀스 번호
	TrackFragments  []TrackFragment  // 트랙 프래그먼트들
	TotalSize       uint32           // 전체 크기
}

// InitializationSegment 초기화 세그먼트 구조
type InitializationSegment struct {
	FTYP []byte // File Type Box
	MOOV []byte // Movie Box
}

// GetCompatibleBrands 호환 브랜드 목록 반환
func GetCompatibleBrands() []string {
	return []string{BrandISOBM, BrandAVC1, BrandMP41, BrandMP42, BrandDASH, BrandMSE1}
}

// GetVideoSampleEntryDefaults 비디오 샘플 엔트리 기본값
func GetVideoSampleEntryDefaults() VideoSampleEntry {
	return VideoSampleEntry{
		SampleEntry: SampleEntry{
			DataRefIndex: 1,
		},
		Width:          1280,
		Height:         720,
		HorizResolution: 0x00480000, // 72 DPI
		VertResolution:  0x00480000, // 72 DPI
		FrameCount:     1,
		Depth:          24,
		PreDefined3:    -1,
	}
}

// GetAudioSampleEntryDefaults 오디오 샘플 엔트리 기본값  
func GetAudioSampleEntryDefaults() AudioSampleEntry {
	return AudioSampleEntry{
		SampleEntry: SampleEntry{
			DataRefIndex: 1,
		},
		ChannelCount: 2,      // 스테레오
		SampleSize:   16,     // 16비트
		SampleRate:   48000 << 16, // 48kHz, 상위 16비트만 사용
	}
}