package mpegts

// parseTSPacket 188바이트 TS 패킷을 파싱
func parseTSPacket(data []byte) (*TSPacket, error) {
	if len(data) < TSPacketSize {
		return nil, &ParseError{
			Err:    ErrInvalidPacketSize,
			Offset: 0,
			Data:   data,
		}
	}

	// 동기 바이트 확인
	if data[0] != SyncByte {
		return nil, &ParseError{
			Err:    ErrInvalidSyncByte,
			Offset: 0,
			Data:   data[:4],
		}
	}

	packet := &TSPacket{}

	// 헤더 파싱
	packet.Header.SyncByte = data[0]
	
	// 두 번째, 세 번째 바이트에서 플래그와 PID 추출
	b1 := data[1]
	b2 := data[2]
	
	packet.Header.TransportErrorIndicator = (b1 & 0x80) != 0
	packet.Header.PayloadUnitStartIndicator = (b1 & 0x40) != 0
	packet.Header.TransportPriority = (b1 & 0x20) != 0
	packet.Header.PID = uint16(b1&0x1F)<<8 | uint16(b2)

	// 네 번째 바이트에서 나머지 필드 추출
	b3 := data[3]
	packet.Header.TransportScramblingControl = (b3 & 0xC0) >> 6
	packet.Header.AdaptationFieldControl = (b3 & 0x30) >> 4
	packet.Header.ContinuityCounter = b3 & 0x0F

	offset := 4 // 헤더 다음부터

	// 적응 필드 처리
	if packet.Header.AdaptationFieldControl == 0x02 || packet.Header.AdaptationFieldControl == 0x03 {
		// 적응 필드 있음
		if offset >= len(data) {
			return nil, &ParseError{
				Err:    ErrBufferTooSmall,
				Offset: offset,
				Data:   data,
			}
		}

		af, consumed, err := parseAdaptationField(data[offset:])
		if err != nil {
			return nil, &ParseError{
				Err:    err,
				Offset: offset,
				Data:   data,
			}
		}

		packet.AdaptationField = af
		offset += consumed
	}

	// 페이로드 처리
	if packet.Header.AdaptationFieldControl == 0x01 || packet.Header.AdaptationFieldControl == 0x03 {
		// 페이로드 있음
		if offset < len(data) {
			packet.Payload = data[offset:]
		}
	}

	return packet, nil
}

// parseAdaptationField 적응 필드 파싱
func parseAdaptationField(data []byte) (*AdaptationField, int, error) {
	if len(data) < 1 {
		return nil, 0, ErrBufferTooSmall
	}

	af := &AdaptationField{}
	af.Length = data[0]

	if af.Length == 0 {
		return af, 1, nil // 길이가 0인 적응 필드
	}

	if len(data) < int(af.Length)+1 {
		return nil, 0, ErrBufferTooSmall
	}

	if af.Length > 0 {
		flags := data[1]
		af.DiscontinuityIndicator = (flags & 0x80) != 0
		af.RandomAccessIndicator = (flags & 0x40) != 0
		af.ElementaryStreamPriority = (flags & 0x20) != 0
		af.PCRFlag = (flags & 0x10) != 0
		af.OPCRFlag = (flags & 0x08) != 0
		af.SplicingPointFlag = (flags & 0x04) != 0
		af.TransportPrivateDataFlag = (flags & 0x02) != 0
		af.AdaptationFieldExtension = (flags & 0x01) != 0

		offset := 2

		// PCR 파싱
		if af.PCRFlag {
			if offset+6 > int(af.Length)+1 {
				return nil, 0, ErrBufferTooSmall
			}

			// PCR은 48비트: 33비트 PCR_base + 9비트 PCR_ext
			pcrData := data[offset : offset+6]
			pcrBase := uint64(pcrData[0])<<25 | uint64(pcrData[1])<<17 | uint64(pcrData[2])<<9 | uint64(pcrData[3])<<1 | uint64(pcrData[4]>>7)
			pcrExt := uint64(pcrData[4]&0x01)<<8 | uint64(pcrData[5])
			af.PCR = pcrBase*300 + pcrExt

			offset += 6
		}

		// OPCR 파싱
		if af.OPCRFlag {
			if offset+6 > int(af.Length)+1 {
				return nil, 0, ErrBufferTooSmall
			}

			// OPCR도 PCR과 같은 형식
			opcrData := data[offset : offset+6]
			opcrBase := uint64(opcrData[0])<<25 | uint64(opcrData[1])<<17 | uint64(opcrData[2])<<9 | uint64(opcrData[3])<<1 | uint64(opcrData[4]>>7)
			opcrExt := uint64(opcrData[4]&0x01)<<8 | uint64(opcrData[5])
			af.OPCR = opcrBase*300 + opcrExt

			offset += 6
		}

		// 스플라이싱 포인트 카운트다운
		if af.SplicingPointFlag {
			if offset+1 > int(af.Length)+1 {
				return nil, 0, ErrBufferTooSmall
			}
			af.SpliceCountdown = int8(data[offset])
			offset++
		}

		// 개인 데이터
		if af.TransportPrivateDataFlag {
			if offset+1 > int(af.Length)+1 {
				return nil, 0, ErrBufferTooSmall
			}
			privateDataLength := data[offset]
			offset++

			if offset+int(privateDataLength) > int(af.Length)+1 {
				return nil, 0, ErrBufferTooSmall
			}
			af.PrivateData = data[offset : offset+int(privateDataLength)]
			offset += int(privateDataLength)
		}
	}

	return af, int(af.Length) + 1, nil
}

// createTSPacket TS 패킷 생성
func createTSPacket(header TSPacketHeader, adaptationField *AdaptationField, payload []byte) ([]byte, error) {
	packet := make([]byte, TSPacketSize)

	// 헤더 생성
	packet[0] = SyncByte

	// 두 번째 바이트: TEI, PUSI, TP, PID 상위 5비트
	packet[1] = 0
	if header.TransportErrorIndicator {
		packet[1] |= 0x80
	}
	if header.PayloadUnitStartIndicator {
		packet[1] |= 0x40
	}
	if header.TransportPriority {
		packet[1] |= 0x20
	}
	packet[1] |= uint8((header.PID >> 8) & 0x1F)

	// 세 번째 바이트: PID 하위 8비트
	packet[2] = uint8(header.PID & 0xFF)

	// 네 번째 바이트: TSC, AFC, CC
	packet[3] = (header.TransportScramblingControl << 6) |
		(header.AdaptationFieldControl << 4) |
		(header.ContinuityCounter & 0x0F)

	offset := 4

	// 적응 필드 추가
	if adaptationField != nil && (header.AdaptationFieldControl == 0x02 || header.AdaptationFieldControl == 0x03) {
		consumed, err := createAdaptationField(packet[offset:], adaptationField)
		if err != nil {
			return nil, err
		}
		offset += consumed
	}

	// 페이로드 추가
	if payload != nil && (header.AdaptationFieldControl == 0x01 || header.AdaptationFieldControl == 0x03) {
		payloadLen := len(payload)
		availableSpace := TSPacketSize - offset

		if payloadLen > availableSpace {
			// 페이로드가 너무 큰 경우 잘라내기
			payloadLen = availableSpace
		}

		copy(packet[offset:offset+payloadLen], payload[:payloadLen])
		offset += payloadLen
	}

	// 남은 공간을 0xFF로 채우기 (스터핑)
	for i := offset; i < TSPacketSize; i++ {
		packet[i] = 0xFF
	}

	return packet, nil
}

// createAdaptationField 적응 필드 생성
func createAdaptationField(data []byte, af *AdaptationField) (int, error) {
	if len(data) < 1 {
		return 0, ErrBufferTooSmall
	}

	// 적응 필드 길이 계산
	length := 1 // 플래그 바이트

	if af.PCRFlag {
		length += 6
	}
	if af.OPCRFlag {
		length += 6
	}
	if af.SplicingPointFlag {
		length += 1
	}
	if af.TransportPrivateDataFlag {
		length += 1 + len(af.PrivateData) // 길이 바이트 + 데이터
	}

	if len(data) < length+1 {
		return 0, ErrBufferTooSmall
	}

	data[0] = uint8(length) // 적응 필드 길이
	offset := 1

	if length > 0 {
		// 플래그 바이트 생성
		flags := uint8(0)
		if af.DiscontinuityIndicator {
			flags |= 0x80
		}
		if af.RandomAccessIndicator {
			flags |= 0x40
		}
		if af.ElementaryStreamPriority {
			flags |= 0x20
		}
		if af.PCRFlag {
			flags |= 0x10
		}
		if af.OPCRFlag {
			flags |= 0x08
		}
		if af.SplicingPointFlag {
			flags |= 0x04
		}
		if af.TransportPrivateDataFlag {
			flags |= 0x02
		}
		if af.AdaptationFieldExtension {
			flags |= 0x01
		}

		data[offset] = flags
		offset++

		// PCR 생성
		if af.PCRFlag {
			pcrBase := af.PCR / 300
			pcrExt := af.PCR % 300

			data[offset] = uint8(pcrBase >> 25)
			data[offset+1] = uint8(pcrBase >> 17)
			data[offset+2] = uint8(pcrBase >> 9)
			data[offset+3] = uint8(pcrBase >> 1)
			data[offset+4] = uint8((pcrBase&1)<<7) | uint8(pcrExt>>8)
			data[offset+5] = uint8(pcrExt & 0xFF)

			offset += 6
		}

		// OPCR 생성
		if af.OPCRFlag {
			opcrBase := af.OPCR / 300
			opcrExt := af.OPCR % 300

			data[offset] = uint8(opcrBase >> 25)
			data[offset+1] = uint8(opcrBase >> 17)
			data[offset+2] = uint8(opcrBase >> 9)
			data[offset+3] = uint8(opcrBase >> 1)
			data[offset+4] = uint8((opcrBase&1)<<7) | uint8(opcrExt>>8)
			data[offset+5] = uint8(opcrExt & 0xFF)

			offset += 6
		}

		// 스플라이싱 포인트 카운트다운
		if af.SplicingPointFlag {
			data[offset] = uint8(af.SpliceCountdown)
			offset++
		}

		// 개인 데이터
		if af.TransportPrivateDataFlag {
			data[offset] = uint8(len(af.PrivateData))
			offset++
			copy(data[offset:offset+len(af.PrivateData)], af.PrivateData)
			offset += len(af.PrivateData)
		}
	}

	return length + 1, nil
}

// GetPCRTimestamp PCR에서 타임스탬프 추출 (마이크로초 단위)
func (af *AdaptationField) GetPCRTimestamp() uint64 {
	if !af.PCRFlag {
		return 0
	}
	// PCR은 27MHz 클록 기준, 마이크로초로 변환: PCR / 27
	return af.PCR / 27
}

// SetPCRTimestamp 타임스탬프를 PCR로 설정 (마이크로초 단위)
func (af *AdaptationField) SetPCRTimestamp(timestampUs uint64) {
	af.PCRFlag = true
	// 마이크로초를 27MHz 클록으로 변환: timestamp * 27
	af.PCR = timestampUs * 27
}

// HasPayload 패킷이 페이로드를 가지는지 확인
func (packet *TSPacket) HasPayload() bool {
	return packet.Header.AdaptationFieldControl == 0x01 || packet.Header.AdaptationFieldControl == 0x03
}

// HasAdaptationField 패킷이 적응 필드를 가지는지 확인
func (packet *TSPacket) HasAdaptationField() bool {
	return packet.Header.AdaptationFieldControl == 0x02 || packet.Header.AdaptationFieldControl == 0x03
}

// IsRandomAccess 랜덤 액세스 포인트인지 확인
func (packet *TSPacket) IsRandomAccess() bool {
	return packet.AdaptationField != nil && packet.AdaptationField.RandomAccessIndicator
}

// GetTimestamp 패킷의 타임스탬프 추출 (PCR이 있는 경우)
func (packet *TSPacket) GetTimestamp() uint32 {
	if packet.AdaptationField != nil && packet.AdaptationField.PCRFlag {
		// PCR을 밀리초로 변환
		return uint32(packet.AdaptationField.GetPCRTimestamp() / 1000)
	}
	return 0
}