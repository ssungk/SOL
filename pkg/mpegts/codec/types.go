package codec

// StreamType 코덱 스트림 타입 (mpegts 패키지와 동일)
type StreamType uint8

const (
	StreamTypeH264 StreamType = 0x1B // H.264/AVC
	StreamTypeH265 StreamType = 0x24 // H.265/HEVC
	StreamTypeADTS StreamType = 0x0F // AAC ADTS
	StreamTypeAAC  StreamType = 0x11 // AAC LATM
)

// NALUType NALU 타입 (mpegts 패키지와 동일)
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

// NALU 구조체 (mpegts 패키지와 동일)
type NALU struct {
	Type     NALUType // NALU 타입
	Data     []byte   // NALU 데이터 (Start Code 제외)
	IsConfig bool     // 설정 NALU 여부 (SPS/PPS/VPS)
	IsKey    bool     // 키프레임 NALU 여부
}

// CodecData 코덱 설정 데이터 (mpegts 패키지와 동일)
type CodecData struct {
	Type       StreamType // 코덱 타입
	SPS        []byte     // Sequence Parameter Set (H.264/H.265)
	PPS        []byte     // Picture Parameter Set (H.264/H.265)
	VPS        []byte     // Video Parameter Set (H.265만)
	ASC        []byte     // Audio Specific Config (AAC)
	Profile    uint8      // 프로파일
	Level      uint8      // 레벨
	Width      uint16     // 비디오 너비
	Height     uint16     // 비디오 높이
	FPS        float64    // 프레임 레이트
	Channels   uint8      // 오디오 채널 수
	SampleRate uint32     // 샘플링 레이트
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