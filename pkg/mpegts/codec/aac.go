package codec

// AAC 프로파일 상수
const (
	AACProfileMain = 1
	AACProfileLC   = 2 // Low Complexity (가장 일반적)
	AACProfileSSR  = 3
	AACProfileLTP  = 4
)

// AAC 샘플링 레이트 테이블
var aacSamplingRates = []uint32{
	96000, 88200, 64000, 48000, 44100, 32000,
	24000, 22050, 16000, 12000, 11025, 8000, 7350,
}

// ADTS 헤더 구조체
type ADTSHeader struct {
	SyncWord                 uint16 // 동기 워드 (0xFFF)
	ID                       uint8  // MPEG Version (0=MPEG-4, 1=MPEG-2)
	Layer                    uint8  // Layer (always 0)
	ProtectionAbsent         bool   // CRC 보호 없음
	Profile                  uint8  // AAC 프로파일
	SamplingFrequencyIndex   uint8  // 샘플링 주파수 인덱스
	PrivateBit               bool   // 개인 비트
	ChannelConfiguration     uint8  // 채널 설정
	OriginalCopy             bool   // 원본/복사본
	Home                     bool   // 홈 비트
	CopyrightIdentificationBit bool   // 저작권 식별 비트
	CopyrightIdentificationStart bool // 저작권 식별 시작
	FrameLength              uint16 // 프레임 길이 (헤더 포함)
	BufferFullness           uint16 // 버퍼 풀니스
	NumRawDataBlocks         uint8  // 원시 데이터 블록 수
}

// Audio Specific Config 구조체
type AudioSpecificConfig struct {
	AudioObjectType       uint8  // 오디오 객체 타입
	SamplingFrequencyIndex uint8  // 샘플링 주파수 인덱스
	SamplingFrequency     uint32 // 실제 샘플링 주파수
	ChannelConfiguration  uint8  // 채널 설정
	FrameLengthFlag       bool   // 프레임 길이 플래그 (0=1024, 1=960 samples)
	DependsOnCoreCoder    bool   // 코어 코더 종속성
	ExtensionFlag         bool   // 확장 플래그
}

// ParseAACFrames AAC 스트림에서 ADTS 프레임들을 파싱
func ParseAACFrames(data []byte) ([]NALU, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var frames []NALU
	offset := 0

	for offset < len(data) {
		// ADTS 동기 워드 찾기 (0xFFF)
		syncPos := findADTSSync(data[offset:])
		if syncPos == -1 {
			break // 더 이상 프레임이 없음
		}

		offset += syncPos

		// ADTS 헤더 파싱
		header, headerSize, err := parseADTSHeader(data[offset:])
		if err != nil {
			offset++
			continue // 다음 위치에서 다시 시도
		}

		// 프레임 데이터 추출
		frameEnd := offset + int(header.FrameLength)
		if frameEnd > len(data) {
			break // 불완전한 프레임
		}

		// AAC 원시 데이터 (ADTS 헤더 제외)
		aacData := data[offset+headerSize : frameEnd]

		// NALU 구조체로 변환 (AAC는 실제로 NALU가 아니지만 호환을 위해 사용)
		frame := NALU{
			Type:     NALUType(0), // AAC는 타입이 없음
			Data:     aacData,
			IsConfig: false, // ADTS는 설정 정보가 헤더에 포함됨
			IsKey:    true,  // 오디오 프레임은 모두 "키프레임"으로 간주
		}

		frames = append(frames, frame)
		offset = frameEnd
	}

	return frames, nil
}

// findADTSSync ADTS 동기 워드 (0xFFF) 위치 찾기
func findADTSSync(data []byte) int {
	for i := 0; i < len(data)-1; i++ {
		// 0xFFF 패턴 확인 (12비트)
		if data[i] == 0xFF && (data[i+1]&0xF0) == 0xF0 {
			return i
		}
	}
	return -1
}

// parseADTSHeader ADTS 헤더 파싱
func parseADTSHeader(data []byte) (*ADTSHeader, int, error) {
	if len(data) < 7 {
		return nil, 0, ErrInvalidADTS
	}

	header := &ADTSHeader{}

	// 동기 워드 확인 (12비트 0xFFF)
	syncWord := uint16(data[0])<<4 | uint16(data[1]>>4)
	if syncWord != 0xFFF {
		return nil, 0, ErrInvalidADTS
	}
	header.SyncWord = syncWord

	// 첫 번째와 두 번째 바이트
	header.ID = (data[1] & 0x08) >> 3
	header.Layer = (data[1] & 0x06) >> 1
	header.ProtectionAbsent = (data[1] & 0x01) != 0

	// 세 번째 바이트
	header.Profile = (data[2] & 0xC0) >> 6
	header.SamplingFrequencyIndex = (data[2] & 0x3C) >> 2
	header.PrivateBit = (data[2] & 0x02) != 0
	header.ChannelConfiguration = ((data[2] & 0x01) << 2) | ((data[3] & 0xC0) >> 6)

	// 네 번째 바이트
	header.OriginalCopy = (data[3] & 0x20) != 0
	header.Home = (data[3] & 0x10) != 0
	header.CopyrightIdentificationBit = (data[3] & 0x08) != 0
	header.CopyrightIdentificationStart = (data[3] & 0x04) != 0

	// 프레임 길이 (13비트)
	header.FrameLength = uint16(data[3]&0x03)<<11 | uint16(data[4])<<3 | uint16(data[5]>>5)

	// 버퍼 풀니스 (11비트)
	header.BufferFullness = uint16(data[5]&0x1F)<<6 | uint16(data[6]>>2)

	// 원시 데이터 블록 수 (2비트)
	header.NumRawDataBlocks = data[6] & 0x03

	// CRC가 있는 경우 헤더 크기는 9바이트, 없으면 7바이트
	headerSize := 7
	if !header.ProtectionAbsent {
		headerSize = 9
		if len(data) < headerSize {
			return nil, 0, ErrInvalidADTS
		}
	}

	return header, headerSize, nil
}

// CreateAudioSpecificConfig ADTS 헤더에서 Audio Specific Config 생성
func CreateAudioSpecificConfig(header *ADTSHeader) (*AudioSpecificConfig, []byte) {
	asc := &AudioSpecificConfig{
		AudioObjectType:       header.Profile + 1, // ADTS profile은 실제값-1
		SamplingFrequencyIndex: header.SamplingFrequencyIndex,
		ChannelConfiguration:  header.ChannelConfiguration,
		FrameLengthFlag:       false, // 1024 samples (일반적인 경우)
		DependsOnCoreCoder:    false,
		ExtensionFlag:         false,
	}

	// 샘플링 주파수 설정
	if int(asc.SamplingFrequencyIndex) < len(aacSamplingRates) {
		asc.SamplingFrequency = aacSamplingRates[asc.SamplingFrequencyIndex]
	}

	// Audio Specific Config 바이너리 데이터 생성 (2바이트)
	ascData := make([]byte, 2)
	ascData[0] = (asc.AudioObjectType << 3) | (asc.SamplingFrequencyIndex >> 1)
	ascData[1] = (asc.SamplingFrequencyIndex << 7) | (asc.ChannelConfiguration << 3)

	return asc, ascData
}

// ExtractAACConfig AAC 프레임들에서 설정 정보 추출
func ExtractAACConfig(frames []NALU, adtsHeader *ADTSHeader) *CodecData {
	codecData := &CodecData{
		Type: StreamTypeADTS,
	}

	if adtsHeader != nil {
		asc, ascData := CreateAudioSpecificConfig(adtsHeader)

		codecData.ASC = ascData
		codecData.Channels = getChannelCount(asc.ChannelConfiguration)
		codecData.SampleRate = asc.SamplingFrequency
		codecData.Profile = asc.AudioObjectType
	}

	return codecData
}

// getChannelCount 채널 설정에서 실제 채널 수 반환
func getChannelCount(channelConfig uint8) uint8 {
	switch channelConfig {
	case 0:
		return 0 // Defined in AOT Specific Config
	case 1:
		return 1 // Mono
	case 2:
		return 2 // Stereo
	case 3:
		return 3 // 3 channels
	case 4:
		return 4 // 4 channels
	case 5:
		return 5 // 5 channels
	case 6:
		return 6 // 5.1 channels
	case 7:
		return 8 // 7.1 channels
	default:
		return 2 // 기본값 스테레오
	}
}

// IsValidADTSHeader ADTS 헤더 유효성 검사
func IsValidADTSHeader(header *ADTSHeader) bool {
	// 기본 검증
	if header.SyncWord != 0xFFF {
		return false
	}

	// 샘플링 주파수 인덱스 검증
	if header.SamplingFrequencyIndex >= uint8(len(aacSamplingRates)) {
		return false
	}

	// 프로파일 검증
	if header.Profile > 3 {
		return false
	}

	// 채널 설정 검증
	if header.ChannelConfiguration > 7 {
		return false
	}

	// 프레임 길이 검증 (최소값)
	if header.FrameLength < 7 {
		return false
	}

	return true
}

// GetAACFrameInfo AAC 프레임 정보 반환
func GetAACFrameInfo(header *ADTSHeader) (sampleRate uint32, channels uint8, profile uint8) {
	if int(header.SamplingFrequencyIndex) < len(aacSamplingRates) {
		sampleRate = aacSamplingRates[header.SamplingFrequencyIndex]
	}
	channels = getChannelCount(header.ChannelConfiguration)
	profile = header.Profile + 1 // ADTS profile + 1 = 실제 AAC 프로파일

	return
}