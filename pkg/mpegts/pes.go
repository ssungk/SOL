package mpegts

import (
	"encoding/binary"
)

// PES 스트림 ID 상수
const (
	StreamIDVideoMin     = 0xE0 // 비디오 스트림 최소값
	StreamIDVideoMax     = 0xEF // 비디오 스트림 최대값
	StreamIDAudioMin     = 0xC0 // 오디오 스트림 최소값
	StreamIDAudioMax     = 0xDF // 오디오 스트림 최대값
	StreamIDPrivateStream1 = 0xBD // 개인 스트림 1
	StreamIDPrivateStream2 = 0xBF // 개인 스트림 2
)

// parsePESPacket PES 패킷 파싱
func parsePESPacket(data []byte) (*PESPacket, error) {
	if len(data) < 6 {
		return nil, &ParseError{
			Err:    ErrBufferTooSmall,
			Offset: 0,
			Data:   data,
		}
	}

	pes := &PESPacket{}

	// PES 패킷 시작 코드 확인 (0x000001)
	if data[0] != 0x00 || data[1] != 0x00 || data[2] != 0x01 {
		return nil, &ParseError{
			Err:    ErrInvalidNALU, // PES 시작 코드 에러로 재사용
			Offset: 0,
			Data:   data[:6],
		}
	}

	// 스트림 ID
	pes.Header.StreamID = data[3]

	// 패킷 길이
	pes.Header.PacketLength = binary.BigEndian.Uint16(data[4:6])

	offset := 6

	// 스트림 ID에 따른 헤더 처리
	if isVideoStream(pes.Header.StreamID) || isAudioStream(pes.Header.StreamID) || pes.Header.StreamID == StreamIDPrivateStream1 {
		// 표준 PES 헤더 파싱
		if len(data) < offset+3 {
			return nil, &ParseError{
				Err:    ErrBufferTooSmall,
				Offset: offset,
				Data:   data,
			}
		}

		// PES 헤더 첫 번째 바이트
		headerByte1 := data[offset]
		pes.Header.MarkerBits = (headerByte1 & 0xC0) >> 6        // 상위 2비트 (10이어야 함)
		pes.Header.ScramblingControl = (headerByte1 & 0x30) >> 4
		pes.Header.Priority = (headerByte1 & 0x08) != 0
		pes.Header.DataAlignmentIndicator = (headerByte1 & 0x04) != 0
		pes.Header.Copyright = (headerByte1 & 0x02) != 0
		pes.Header.OriginalOrCopy = (headerByte1 & 0x01) != 0

		// PES 헤더 두 번째 바이트
		headerByte2 := data[offset+1]
		pes.Header.PTSDTSFlags = (headerByte2 & 0xC0) >> 6
		pes.Header.ESCRFlag = (headerByte2 & 0x20) != 0
		pes.Header.ESRateFlag = (headerByte2 & 0x10) != 0
		pes.Header.DSMTrickModeFlag = (headerByte2 & 0x08) != 0
		pes.Header.AdditionalCopyInfoFlag = (headerByte2 & 0x04) != 0
		pes.Header.CRCFlag = (headerByte2 & 0x02) != 0
		pes.Header.ExtensionFlag = (headerByte2 & 0x01) != 0

		// PES 헤더 데이터 길이
		pes.Header.HeaderDataLength = data[offset+2]

		offset += 3

		// 선택적 필드들 파싱
		optionalFieldsEnd := offset + int(pes.Header.HeaderDataLength)
		if optionalFieldsEnd > len(data) {
			return nil, &ParseError{
				Err:    ErrBufferTooSmall,
				Offset: offset,
				Data:   data,
			}
		}

		// PTS/DTS 파싱
		if pes.Header.PTSDTSFlags == 0x02 || pes.Header.PTSDTSFlags == 0x03 {
			// PTS 존재
			if offset+5 > optionalFieldsEnd {
				return nil, &ParseError{
					Err:    ErrBufferTooSmall,
					Offset: offset,
					Data:   data,
				}
			}

			pes.Header.PTS = parsePTSDTS(data[offset : offset+5])
			offset += 5
		}

		if pes.Header.PTSDTSFlags == 0x03 {
			// DTS 존재
			if offset+5 > optionalFieldsEnd {
				return nil, &ParseError{
					Err:    ErrBufferTooSmall,
					Offset: offset,
					Data:   data,
				}
			}

			pes.Header.DTS = parsePTSDTS(data[offset : offset+5])
			offset += 5
		}

		// 나머지 선택적 필드들은 건너뛰기 (향후 필요시 구현)
		offset = optionalFieldsEnd

		// 페이로드 시작 위치 설정
		pes.StartOffset = offset
	} else {
		// 비표준 스트림 (시작 코드 이후가 바로 데이터)
		pes.StartOffset = offset
	}

	// PES 데이터 추출
	if offset < len(data) {
		pes.Data = data[offset:]
	}

	return pes, nil
}

// parsePTSDTS PTS/DTS 타임스탬프 파싱 (33비트 값)
func parsePTSDTS(data []byte) uint64 {
	if len(data) < 5 {
		return 0
	}

	// PTS/DTS는 33비트 값으로, 다음과 같이 인코딩됨:
	// [7:4] marker | [3:1] PTS[32:30] | [0] marker
	// [7:0] PTS[29:22]
	// [7:1] PTS[21:15] | [0] marker  
	// [7:0] PTS[14:7]
	// [7:1] PTS[6:0] | [0] marker

	timestamp := uint64(data[0]&0x0E) << 29 // 상위 3비트
	timestamp |= uint64(data[1]) << 22      // 다음 8비트
	timestamp |= uint64(data[2]&0xFE) << 14 // 다음 7비트
	timestamp |= uint64(data[3]) << 7       // 다음 8비트
	timestamp |= uint64(data[4]&0xFE) >> 1  // 하위 7비트

	return timestamp
}

// createPESPacket PES 패킷 생성
func createPESPacket(streamID uint8, pts, dts uint64, data []byte) ([]byte, error) {
	// PES 헤더 기본 크기 계산
	headerSize := 9 // 기본 헤더 (6바이트) + 확장 헤더 (3바이트)

	// PTS/DTS 필드 크기 추가
	var ptsDtsFlags uint8
	if pts > 0 && dts > 0 {
		ptsDtsFlags = 0x03 // PTS와 DTS 모두 존재
		headerSize += 10   // PTS(5) + DTS(5)
	} else if pts > 0 {
		ptsDtsFlags = 0x02 // PTS만 존재
		headerSize += 5    // PTS(5)
	}

	// 패킷 전체 크기
	totalSize := headerSize + len(data)
	packet := make([]byte, totalSize)

	offset := 0

	// PES 패킷 시작 코드 (0x000001)
	packet[offset] = 0x00
	packet[offset+1] = 0x00
	packet[offset+2] = 0x01
	packet[offset+3] = streamID
	offset += 4

	// PES 패킷 길이 (시작 코드와 스트림 ID를 제외한 나머지)
	packetLength := totalSize - 6
	if packetLength > 0xFFFF {
		packetLength = 0 // 길이가 65535를 초과하면 0으로 설정 (무제한)
	}
	binary.BigEndian.PutUint16(packet[offset:], uint16(packetLength))
	offset += 2

	// PES 헤더 플래그 (표준 스트림인 경우)
	if isVideoStream(streamID) || isAudioStream(streamID) || streamID == StreamIDPrivateStream1 {
		// 첫 번째 플래그 바이트 (10xxxxxx)
		packet[offset] = 0x80 // marker bits = 10
		offset++

		// 두 번째 플래그 바이트
		packet[offset] = ptsDtsFlags << 6 // PTS/DTS 플래그
		offset++

		// 헤더 데이터 길이
		headerDataLength := headerSize - 9 // 기본 헤더 제외
		packet[offset] = uint8(headerDataLength)
		offset++

		// PTS 추가
		if ptsDtsFlags == 0x02 || ptsDtsFlags == 0x03 {
			var markerBits uint8
			if ptsDtsFlags == 0x03 {
				markerBits = 0x30
			} else {
				markerBits = 0x20
			}
			encodePTSDTS(packet[offset:offset+5], pts, markerBits)
			offset += 5
		}

		// DTS 추가
		if ptsDtsFlags == 0x03 {
			encodePTSDTS(packet[offset:offset+5], dts, 0x10)
			offset += 5
		}
	}

	// 페이로드 데이터 복사
	if len(data) > 0 {
		copy(packet[offset:], data)
	}

	return packet, nil
}

// encodePTSDTS PTS/DTS를 5바이트로 인코딩
func encodePTSDTS(data []byte, timestamp uint64, markerBits uint8) {
	data[0] = markerBits | uint8((timestamp>>29)&0x0E) | 0x01 // marker
	data[1] = uint8(timestamp >> 22)
	data[2] = uint8((timestamp>>14)&0xFE) | 0x01 // marker
	data[3] = uint8(timestamp >> 7)
	data[4] = uint8((timestamp&0x7F)<<1) | 0x01 // marker
}

// isVideoStream 비디오 스트림 ID 여부 확인
func isVideoStream(streamID uint8) bool {
	return streamID >= StreamIDVideoMin && streamID <= StreamIDVideoMax
}

// isAudioStream 오디오 스트림 ID 여부 확인
func isAudioStream(streamID uint8) bool {
	return streamID >= StreamIDAudioMin && streamID <= StreamIDAudioMax
}

// GetTimestampMs PTS/DTS를 밀리초로 변환
func (pes *PESPacket) GetTimestampMs() uint32 {
	if pes.Header.PTS > 0 {
		return ExtractTimestamp(pes.Header.PTS)
	}
	return 0
}

// HasPTS PTS가 있는지 확인
func (pes *PESPacket) HasPTS() bool {
	return pes.Header.PTSDTSFlags == 0x02 || pes.Header.PTSDTSFlags == 0x03
}

// HasDTS DTS가 있는지 확인
func (pes *PESPacket) HasDTS() bool {
	return pes.Header.PTSDTSFlags == 0x03
}

// IsVideoPacket 비디오 PES 패킷인지 확인
func (pes *PESPacket) IsVideoPacket() bool {
	return isVideoStream(pes.Header.StreamID)
}

// IsAudioPacket 오디오 PES 패킷인지 확인
func (pes *PESPacket) IsAudioPacket() bool {
	return isAudioStream(pes.Header.StreamID)
}

// GetElementaryStreamData 엘리멘터리 스트림 데이터 반환 (제로카피)
func (pes *PESPacket) GetElementaryStreamData() []byte {
	return pes.Data
}

// GetElementaryStreamDataChunks 엘리멘터리 스트림 데이터를 청크 배열로 반환 (제로카피)
func (pes *PESPacket) GetElementaryStreamDataChunks() [][]byte {
	if len(pes.Data) == 0 {
		return nil
	}
	return [][]byte{pes.Data}
}