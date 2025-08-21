package media


// Codec 비디오/오디오/데이터를 포함하는 통합 코덱 타입
type Codec uint8

const (
	// Unknown/Error detection
	Unknown Codec = 0

	// Video codecs: 1-127
	H264 Codec = 1
	H265 Codec = 2
	VP8  Codec = 3
	VP9  Codec = 4
	AV1  Codec = 5

	// Audio codecs: 128-191
	AAC  Codec = 128
	Opus Codec = 129
	MP3  Codec = 130

	// Data types: 192-255
	//WebVTT Codec = 192 // WebVTT 자막 (HLS/웹 표준)
	//SRT    Codec = 193 // SRT 자막 파일
	//SCTE35 Codec = 194 // SCTE-35 광고 삽입 신호
)

// Codec 헬퍼 함수들
func (c Codec) IsVideo() bool   { return c > 0 && c < 128 }
func (c Codec) IsAudio() bool   { return c >= 128 && c < 192 }
func (c Codec) IsData() bool    { return c >= 192 }
func (c Codec) IsUnknown() bool { return c == 0 }

// BitstreamFormat 비트스트림 포맷 (범용 + 코덱별 명시)
type BitstreamFormat uint8

const (
	// 범용 포맷 (모든 코덱)
	FormatRawStream BitstreamFormat = 0 // 원시 비트스트림
	FormatPackaged  BitstreamFormat = 1 // 패키징된 포맷 (헤더/컨테이너 추가)

	// H26x (H264/H265) 명시적 포맷
	FormatH26xAnnexB BitstreamFormat = 0 // H264/H265 Annex-B (= FormatRawStream)
	FormatH26xAVCC   BitstreamFormat = 1 // H264/H265 AVCC (= FormatPackaged)

	// AAC 명시적 포맷
	FormatAACRaw  BitstreamFormat = 0 // AAC Raw (= FormatRawStream)
	FormatAACADTS BitstreamFormat = 1 // AAC ADTS (= FormatPackaged)
)

// FrameType 프레임 타입 (개념적 분류 기반)
type FrameType uint8

const (
	TypeData   FrameType = 0 // 일반 데이터 (비디오 P/B프레임, 오디오 데이터)
	TypeConfig FrameType = 1 // 설정 데이터 (비디오 SPS/PPS, 오디오 AudioSpecificConfig)
	TypeKey    FrameType = 2 // 키프레임 (비디오 I-프레임만)
)

// Frame 새로운 단순화된 프레임 구조
type Frame struct {
	TrackIndex int             // 트랙 인덱스 (0=비디오, 1=오디오, 2=...)
	Codec      Codec           // 통합된 코덱 (비디오/오디오 구분 포함)
	Format     BitstreamFormat // 코덱별 비트스트림 포맷
	Type       FrameType       // 프레임 타입
	DTS        uint64          // Decode Time Stamp (해당 트랙의 TimeScale 단위)
	CTS        int             // Composition Time Stamp (PTS-DTS, TimeScale 단위)
	Data       []byte          // 프레임 데이터
}

// Frame 헬퍼 함수들
func (f *Frame) IsVideo() bool    { return f.Codec.IsVideo() }
func (f *Frame) IsAudio() bool    { return f.Codec.IsAudio() }
func (f *Frame) IsData() bool     { return f.Codec.IsData() }
func (f *Frame) IsKeyFrame() bool { return f.Type == TypeKey }

// PTS Presentation Time Stamp 계산 (DTS + CTS, TimeScale 단위)
func (f *Frame) PTS() uint64 {
	return uint64(int64(f.DTS) + int64(f.CTS))
}

// DTS32 프로토콜용 32비트 DTS 변환 (현재 TimeScale 단위)
func (f *Frame) DTS32() uint32 {
	return uint32(f.DTS & 0xFFFFFFFF)
}

// NewFrame 미디어 프레임 생성 (CTS 포함)
func NewFrame(trackIndex int, codec Codec, format BitstreamFormat, frameType FrameType, dts uint64, cts int, data []byte) Frame {
	return Frame{
		TrackIndex: trackIndex,
		Codec:      codec,
		Format:     format,
		Type:       frameType,
		DTS:        dts,
		CTS:        cts,
		Data:       data,
	}
}

// ContainsCodec 코덱 목록에서 특정 코덱 포함 여부 확인
func ContainsCodec(codecs []Codec, target Codec) bool {
	for _, codec := range codecs {
		if codec == target {
			return true
		}
	}
	return false
}
