package rtmp

import (
	"sol/pkg/media"
)

// GenerateVideoHeader 비디오 패킷을 위한 RTMP 헤더 생성 (5바이트)
func GenerateVideoHeader(packet media.Packet, cts int) []byte {
	header := make([]byte, 5)

	// Frame Type + Codec ID (1바이트)
	if packet.IsKeyPacket() || packet.Type == media.TypeConfig {
		header[0] = 0x17 // Key frame + AVC
	} else {
		header[0] = 0x27 // Inter frame + AVC
	}

	// AVC Packet Type (1바이트)
	if packet.Type == media.TypeConfig {
		header[1] = 0x00 // AVC sequence header
	} else {
		header[1] = 0x01 // AVC NALU
	}

	// Composition Time (3바이트, big-endian, signed 24-bit)
	// 24비트 범위로 제한하고 2의 보수 형태로 인코딩
	header[2] = byte((cts >> 16) & 0xFF)
	header[3] = byte((cts >> 8) & 0xFF)
	header[4] = byte(cts & 0xFF)

	return header
}

// GenerateAudioHeader 오디오 패킷을 위한 RTMP 헤더 생성 (2바이트)
func GenerateAudioHeader(packet media.Packet) []byte {
	header := make([]byte, 2)

	// Audio Info (1바이트): Sound Format(4bit) + Sound Rate(2bit) + Sound Size(1bit) + Sound Type(1bit)
	// AAC: 1010 + 11 + 1 + 1 = 0xAF
	header[0] = 0xAF

	// AAC Packet Type (1바이트)
	if packet.Type == media.TypeConfig {
		header[1] = 0x00 // AAC sequence header
	} else {
		header[1] = 0x01 // AAC raw data
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

// ParseCompositionTime 3바이트 배열에서 signed 24-bit CTS 추출
func ParseCompositionTime(data []byte) int {
	if len(data) < 3 {
		return 0
	}
	
	// 24비트 값을 uint32로 조합
	value := uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2])
	
	// 24비트 signed 값으로 변환 (2의 보수)
	// 24비트에서 MSB가 1이면 음수
	if value&0x800000 != 0 {
		// 음수인 경우: 32비트로 확장하여 부호 유지
		return int(int32(value | 0xFF000000))
	}
	
	// 양수인 경우: 그대로 반환
	return int(value)
}

// ParseVideoHeader 비디오 헤더에서 정보 추출
func ParseVideoHeader(header []byte) (frameType byte, codecId byte, packetType byte, cts int) {
	if len(header) < 1 {
		return 0, 0, 0, 0
	}
	
	// Frame Type + Codec ID (1바이트)
	frameType = (header[0] & 0xF0) >> 4 // 상위 4비트
	codecId = header[0] & 0x0F          // 하위 4비트
	
	if len(header) < 2 {
		return frameType, codecId, 0, 0
	}
	
	// AVC/HEVC/AV1 Packet Type (1바이트)
	packetType = header[1]
	
	if len(header) >= 5 {
		// Composition Time (3바이트)
		cts = ParseCompositionTime(header[2:5])
	}
	
	return frameType, codecId, packetType, cts
}
