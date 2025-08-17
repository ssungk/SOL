package media

// MediaCodec 비디오/오디오/데이터를 포함하는 통합 코덱 타입
type MediaCodec uint8

const (
    // Unknown/Error detection
    MediaUnknown MediaCodec = 0

    // Video codecs: 1-127
    MediaH264 MediaCodec = 1
    MediaH265 MediaCodec = 2
    MediaVP8  MediaCodec = 3
    MediaVP9  MediaCodec = 4
    MediaAV1  MediaCodec = 5

    // Audio codecs: 128-191
    MediaAAC  MediaCodec = 128
    MediaOpus MediaCodec = 129
    MediaMP3  MediaCodec = 130

    // Data types: 192-255
    MediaWebVTT MediaCodec = 192 // WebVTT 자막 (HLS/웹 표준)
    MediaSRT    MediaCodec = 193 // SRT 자막 파일
    MediaSCTE35 MediaCodec = 194 // SCTE-35 광고 삽입 신호
)

// MediaCodec 헬퍼 함수들
func (c MediaCodec) IsVideo() bool   { return c > 0 && c < 128 }
func (c MediaCodec) IsAudio() bool   { return c >= 128 && c < 192 }
func (c MediaCodec) IsData() bool    { return c >= 192 }
func (c MediaCodec) IsUnknown() bool { return c == 0 }

// BitstreamFormat 비트스트림 포맷 (범용 + 코덱별 명시)
type BitstreamFormat uint8

const (
    // 범용 포맷 (모든 코덱)
    FormatRawStream BitstreamFormat = 0 // 원시 비트스트림
    FormatPackaged BitstreamFormat = 1 // 패키징된 포맷 (헤더/컨테이너 추가)

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

// MediaFrame 새로운 단순화된 프레임 구조
type MediaFrame struct {
    Codec     MediaCodec      // 통합된 코덱 (비디오/오디오 구분 포함)
    Format    BitstreamFormat // 코덱별 비트스트림 포맷
    Type      FrameType       // 프레임 타입
    Timestamp uint32          // 타임스탬프 (밀리초)
    Data      []byte          // 프레임 데이터
}

// MediaFrame 헬퍼 함수들
func (f *MediaFrame) IsVideo() bool    { return f.Codec.IsVideo() }
func (f *MediaFrame) IsAudio() bool    { return f.Codec.IsAudio() }
func (f *MediaFrame) IsData() bool     { return f.Codec.IsData() }
func (f *MediaFrame) IsKeyFrame() bool { return f.Type == TypeKey }

// NewMediaFrame 새로운 미디어 프레임 생성
func NewMediaFrame(codec MediaCodec, format BitstreamFormat, frameType FrameType, timestamp uint32, data []byte) MediaFrame {
    return MediaFrame{
        Codec:     codec,
        Format:    format,
        Type:      frameType,
        Timestamp: timestamp,
        Data:      data,
    }
}


