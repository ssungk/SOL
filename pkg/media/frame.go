package media

// Type represents the type of media
type Type uint8

const (
	TypeAudio Type = iota + 1
	TypeVideo
	TypeMetadata
)

// CodecType 코덱 타입
type CodecType uint8

const (
	CodecUnknown CodecType = iota
	CodecH264
	CodecH265
	CodecAAC
	CodecOpus
	CodecVP8
	CodecVP9
	CodecAV1
)

// FormatType 데이터 포맷 타입
type FormatType uint8

const (
	FormatUnknown FormatType = iota
	FormatRaw               // 원본 데이터
	FormatAVCC              // H264/H265 AVCC 포맷 (length-prefix)
	FormatStartCode         // H264/H265 StartCode 포맷 (0x00 0x00 0x01)
	FormatADTS              // AAC ADTS 포맷
)

// FrameSubType 미디어 프레임의 세부 타입
type FrameSubType uint8

const (
	// Video subtypes
	VideoKeyFrame FrameSubType = iota + 1
	VideoInterFrame
	VideoDisposableInterFrame // B-frame
	VideoInfoFrame
	VideoSequenceHeader // SPS/PPS/VPS 등
	VideoEndOfSequence

	// Audio subtypes
	AudioRawData
	AudioSequenceHeader // Audio Specific Config 등
)

// Frame represents a generic media frame
type Frame struct {
	Type       Type         // Media type (audio/video/metadata)
	SubType    FrameSubType // Frame sub type (VideoKeyFrame, AudioRawData 등)
	CodecType  CodecType    // 코덱 타입 (H264, H265, AAC 등)
	FormatType FormatType   // 데이터 포맷 (AVCC, StartCode 등)
	Timestamp  uint32       // Frame timestamp in milliseconds
	Data       [][]byte     // Zero-copy payload chunks
}

// VideoFrame represents a video frame with video-specific information
type VideoFrame struct {
	Frame
	IsKeyFrame    bool // Whether this is a key frame
	IsConfigFrame bool // Whether this is a config frame (e.g., SPS/PPS/VPS)
}

// AudioFrame represents an audio frame with audio-specific information
type AudioFrame struct {
	Frame
	IsConfigFrame bool // Whether this is a config frame (e.g., Audio Specific Config)
}

// NewVideoFrame creates a new video frame
func NewVideoFrame(subType FrameSubType, codecType CodecType, formatType FormatType, timestamp uint32, data [][]byte) VideoFrame {
	return VideoFrame{
		Frame: Frame{
			Type:       TypeVideo,
			SubType:    subType,
			CodecType:  codecType,
			FormatType: formatType,
			Timestamp:  timestamp,
			Data:       data,
		},
		IsKeyFrame:    IsVideoKeyFrame(subType),
		IsConfigFrame: IsVideoConfigFrame(subType),
	}
}

// NewAudioFrame creates a new audio frame
func NewAudioFrame(subType FrameSubType, codecType CodecType, formatType FormatType, timestamp uint32, data [][]byte) AudioFrame {
	return AudioFrame{
		Frame: Frame{
			Type:       TypeAudio,
			SubType:    subType,
			CodecType:  codecType,
			FormatType: formatType,
			Timestamp:  timestamp,
			Data:       data,
		},
		IsConfigFrame: IsAudioConfigFrame(subType),
	}
}

// IsVideoKeyFrame 비디오 키프레임 여부 확인
func IsVideoKeyFrame(subType FrameSubType) bool {
	return subType == VideoKeyFrame
}

// IsVideoConfigFrame 비디오 설정 프레임 여부 확인
func IsVideoConfigFrame(subType FrameSubType) bool {
	return subType == VideoSequenceHeader
}

// IsAudioConfigFrame 오디오 설정 프레임 여부 확인
func IsAudioConfigFrame(subType FrameSubType) bool {
	return subType == AudioSequenceHeader
}

// ConvertH264Format H264 프레임 포맷 변환
func ConvertH264Format(data [][]byte, fromFormat, toFormat FormatType) ([][]byte, error) {
	if fromFormat == toFormat {
		return data, nil
	}

	switch {
	case fromFormat == FormatAVCC && toFormat == FormatStartCode:
		return convertAVCCToStartCode(data), nil
	case fromFormat == FormatStartCode && toFormat == FormatAVCC:
		return convertStartCodeToAVCC(data), nil
	default:
		return data, nil // 지원하지 않는 변환은 원본 반환
	}
}

// convertAVCCToStartCode AVCC 포맷을 StartCode 포맷으로 변환
func convertAVCCToStartCode(data [][]byte) [][]byte {
	var result [][]byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	for _, chunk := range data {
		pos := 0
		for pos < len(chunk) {
			if pos+4 > len(chunk) {
				break
			}

			// AVCC 길이 읽기 (4바이트 big-endian)
			naluLength := int(chunk[pos])<<24 | int(chunk[pos+1])<<16 | int(chunk[pos+2])<<8 | int(chunk[pos+3])
			pos += 4

			if pos+naluLength > len(chunk) {
				break
			}

			// StartCode + NALU 데이터
			naluWithStartCode := make([]byte, 0, 4+naluLength)
			naluWithStartCode = append(naluWithStartCode, startCode...)
			naluWithStartCode = append(naluWithStartCode, chunk[pos:pos+naluLength]...)
			result = append(result, naluWithStartCode)

			pos += naluLength
		}
	}

	return result
}

// convertStartCodeToAVCC StartCode 포맷을 AVCC 포맷으로 변환
func convertStartCodeToAVCC(data [][]byte) [][]byte {
	var result [][]byte

	for _, chunk := range data {
		pos := 0
		var convertedChunk []byte

		for pos < len(chunk) {
			// StartCode 찾기
			startCodePos := findStartCode(chunk[pos:])
			if startCodePos == -1 {
				break
			}
			startCodePos += pos

			// StartCode 길이 확인
			var startCodeLen int
			if startCodePos+4 <= len(chunk) &&
				chunk[startCodePos] == 0x00 && chunk[startCodePos+1] == 0x00 &&
				chunk[startCodePos+2] == 0x00 && chunk[startCodePos+3] == 0x01 {
				startCodeLen = 4
			} else if startCodePos+3 <= len(chunk) &&
				chunk[startCodePos] == 0x00 && chunk[startCodePos+1] == 0x00 && chunk[startCodePos+2] == 0x01 {
				startCodeLen = 3
			} else {
				break
			}

			naluStart := startCodePos + startCodeLen
			if naluStart >= len(chunk) {
				break
			}

			// 다음 StartCode 찾기
			nextStartCodePos := findStartCode(chunk[naluStart:])
			var naluEnd int
			if nextStartCodePos != -1 {
				naluEnd = naluStart + nextStartCodePos
			} else {
				naluEnd = len(chunk)
			}

			naluLength := naluEnd - naluStart
			if naluLength > 0 {
				// AVCC 헤더 (4바이트 길이) + NALU 데이터
				lengthBytes := []byte{
					byte(naluLength >> 24),
					byte(naluLength >> 16),
					byte(naluLength >> 8),
					byte(naluLength),
				}
				convertedChunk = append(convertedChunk, lengthBytes...)
				convertedChunk = append(convertedChunk, chunk[naluStart:naluEnd]...)
			}

			pos = naluEnd
		}

		if len(convertedChunk) > 0 {
			result = append(result, convertedChunk)
		}
	}

	return result
}

// findStartCode StartCode 위치 찾기
func findStartCode(data []byte) int {
	for i := 0; i < len(data)-2; i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			return i
		}
		if i < len(data)-3 && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			return i
		}
	}
	return -1
}