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
		// Composition Time (3바이트, signed 24-bit)
		data := header[2:5]
		value := uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2])
		if value&0x800000 != 0 {
			cts = int(int32(value | 0xFF000000))
		} else {
			cts = int(value)
		}
	}
	
	return frameType, codecId, packetType, cts
}
