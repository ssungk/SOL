package core


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
	FormatAACADTS BitstreamFormat = 0 // AAC ADTS (= FormatRawStream)
	FormatAACRaw  BitstreamFormat = 1 // AAC Raw (= FormatPackaged)
)

// PacketType 패킷 타입 (개념적 분류 기반)
type PacketType uint8

const (
	TypeData   PacketType = 0 // 일반 데이터 (비디오 P/B패킷, 오디오 데이터)
	TypeConfig PacketType = 1 // 설정 데이터 (비디오 SPS/PPS, 오디오 AudioSpecificConfig)
	TypeKey    PacketType = 2 // 키패킷 (비디오 I-패킷만)
)

// Packet 새로운 단순화된 패킷 구조
type Packet struct {
	TrackIndex int             // 트랙 인덱스 (0=비디오, 1=오디오, 2=...)
	Codec      Codec           // 통합된 코덱 (비디오/오디오 구분 포함)
	Format     BitstreamFormat // 코덱별 비트스트림 포맷
	Type       PacketType      // 패킷 타입
	DTS        uint64          // Decode Time Stamp (해당 트랙의 TimeScale 단위)
	CTS        int             // Composition Time Stamp (PTS-DTS, TimeScale 단위)
	Data       []*Buffer       // 패킷 데이터
}

// Packet 헬퍼 함수들
func (p *Packet) IsVideo() bool    { return p.Codec.IsVideo() }
func (p *Packet) IsAudio() bool    { return p.Codec.IsAudio() }
func (p *Packet) IsData() bool     { return p.Codec.IsData() }
func (p *Packet) IsKeyPacket() bool { return p.Type == TypeKey }

// PTS Presentation Time Stamp 계산 (DTS + CTS, TimeScale 단위)
func (p *Packet) PTS() uint64 {
	return uint64(int64(p.DTS) + int64(p.CTS))
}

// DTS32 프로토콜용 32비트 DTS 변환 (현재 TimeScale 단위)
func (p *Packet) DTS32() uint32 {
	return uint32(p.DTS & 0xFFFFFFFF)
}

// Release 패킷의 모든 버퍼 해제
func (p *Packet) Release() {
	for _, buffer := range p.Data {
		buffer.Release()
	}
}


// NewPacket 미디어 패킷 생성 (CTS 포함)
func NewPacket(trackIndex int, codec Codec, format BitstreamFormat, packetType PacketType, dts uint64, cts int, data []*Buffer) Packet {
	return Packet{
		TrackIndex: trackIndex,
		Codec:      codec,
		Format:     format,
		Type:       packetType,
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