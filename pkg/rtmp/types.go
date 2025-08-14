package rtmp

import (
	"sol/pkg/media"
)

// GenerateVideoHeader 비디오 프레임을 위한 RTMP 헤더 생성 (5바이트)
func GenerateVideoHeader(frameSubType media.FrameSubType, compositionTime uint32) []byte {
	header := make([]byte, 5)

	// Frame Type + Codec ID (1바이트)
	switch frameSubType {
	case media.VideoKeyFrame:
		header[0] = 0x17 // Key frame + AVC
	case media.VideoInterFrame:
		header[0] = 0x27 // Inter frame + AVC
	case media.VideoDisposableInterFrame:
		header[0] = 0x37 // Disposable inter frame + AVC
	case media.VideoInfoFrame:
		header[0] = 0x57 // Video info frame + AVC
	case media.VideoSequenceHeader:
		header[0] = 0x17 // Key frame + AVC
	case media.VideoEndOfSequence:
		header[0] = 0x17 // Key frame + AVC
	default:
		header[0] = 0x27 // 기본값: Inter frame + AVC
	}

	// AVC Packet Type (1바이트)
	switch frameSubType {
	case media.VideoSequenceHeader:
		header[1] = 0x00 // AVC sequence header
	case media.VideoEndOfSequence:
		header[1] = 0x02 // AVC end of sequence
	default:
		header[1] = 0x01 // AVC NALU
	}

	// Composition Time (3바이트, big-endian)
	header[2] = byte((compositionTime >> 16) & 0xFF)
	header[3] = byte((compositionTime >> 8) & 0xFF)
	header[4] = byte(compositionTime & 0xFF)

	return header
}

// GenerateAudioHeader 오디오 프레임을 위한 RTMP 헤더 생성 (2바이트)
func GenerateAudioHeader(frameSubType media.FrameSubType) []byte {
	header := make([]byte, 2)

	// Audio Info (1바이트): Sound Format(4bit) + Sound Rate(2bit) + Sound Size(1bit) + Sound Type(1bit)
	// AAC: 1010 + 11 + 1 + 1 = 0xAF
	header[0] = 0xAF

	// AAC Packet Type (1바이트)
	switch frameSubType {
	case media.AudioSequenceHeader:
		header[1] = 0x00 // AAC sequence header
	case media.AudioRawData:
		header[1] = 0x01 // AAC raw data
	default:
		header[1] = 0x01 // 기본값: AAC raw data
	}

	return header
}

// CombineHeaderAndData RTMP 헤더와 데이터를 결합하여 새로운 []byte 생성
func CombineHeaderAndData(header []byte, data []byte) []byte {
	if len(data) == 0 {
		return header
	}

	// 헤더와 데이터를 결합한 새로운 버퍼 생성
	combined := make([]byte, len(header)+len(data))
	copy(combined, header)
	copy(combined[len(header):], data)

	return combined
}
