package codec

// H.265 파라미터 셋 정보
type H265VPS struct {
	VideoParameterSetID uint8  // VPS ID
	MaxLayersMinus1     uint8  // 최대 레이어 수 - 1
	MaxSubLayersMinus1  uint8  // 최대 서브 레이어 수 - 1
	TemporalIDNesting   bool   // 시간적 ID 네스팅
}

type H265SPS struct {
	VideoParameterSetID    uint8  // 참조하는 VPS ID
	SeqParameterSetID      uint8  // SPS ID
	ChromaFormatIDC        uint8  // 크로마 포맷
	PicWidthInLumaSamples  uint32 // 루마 샘플 단위 너비
	PicHeightInLumaSamples uint32 // 루마 샘플 단위 높이
	Width                  uint16 // 실제 너비 (픽셀)
	Height                 uint16 // 실제 높이 (픽셀)
	BitDepthLuma           uint8  // 루마 비트 깊이
	BitDepthChroma         uint8  // 크로마 비트 깊이
}

type H265PPS struct {
	PicParameterSetID     uint8 // PPS ID
	SeqParameterSetID     uint8 // 참조하는 SPS ID
	DependentSliceSegments bool  // 종속 슬라이스 세그먼트
	OutputFlagPresent     bool  // 출력 플래그 존재
	NumExtraSliceHeader   uint8 // 추가 슬라이스 헤더 수
}

// ParseH265NALUs H.265 스트림에서 NALU들을 파싱
func ParseH265NALUs(data []byte) ([]NALU, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var nalus []NALU
	
	// H.264와 동일한 시작 코드 사용
	positions := findStartCodes(data)
	
	for i, pos := range positions {
		var naluData []byte
		
		if i+1 < len(positions) {
			naluData = data[pos:positions[i+1]]
		} else {
			naluData = data[pos:]
		}
		
		// 시작 코드 제거
		startCodeLen := getStartCodeLength(naluData)
		if startCodeLen == 0 || len(naluData) <= startCodeLen {
			continue
		}
		
		naluPayload := naluData[startCodeLen:]
		if len(naluPayload) < 2 {
			continue // H.265는 최소 2바이트 헤더 필요
		}
		
		// H.265 NALU 헤더 파싱 (2바이트)
		naluHeader1 := naluPayload[0]
		naluHeader2 := naluPayload[1]
		
		forbiddenBit := (naluHeader1 & 0x80) != 0
		if forbiddenBit {
			continue
		}
		
		naluType := NALUType((naluHeader1 & 0x7E) >> 1) // 6비트
		layerID := ((naluHeader1 & 0x01) << 5) | ((naluHeader2 & 0xF8) >> 3) // 6비트
		temporalID := (naluHeader2 & 0x07) - 1 // 3비트에서 1을 빼야 실제 temporal_id
		
		_ = layerID   // 향후 사용
		_ = temporalID // 향후 사용
		
		// NALU 생성
		nalu := NALU{
			Type:     naluType,
			Data:     naluPayload,
			IsConfig: naluType.IsH265Config(),
			IsKey:    naluType.IsH265KeyFrame(),
		}
		
		nalus = append(nalus, nalu)
	}
	
	return nalus, nil
}

// ParseH265VPS VPS NALU에서 비디오 파라미터 추출
func ParseH265VPS(naluData []byte) (*H265VPS, error) {
	if len(naluData) < 3 {
		return nil, ErrInvalidNALU
	}
	
	// NALU 타입 확인
	naluType := NALUType((naluData[0] & 0x7E) >> 1)
	if naluType != H265NALUTypeVPS {
		return nil, ErrInvalidNALU
	}
	
	vps := &H265VPS{}
	
	// 기본적인 VPS 파싱 (실제로는 더 복잡한 비트 스트림 파싱이 필요)
	// 여기서는 간단한 추출만 구현
	payload := naluData[2:] // NALU 헤더 (2바이트) 제외
	if len(payload) > 0 {
		vps.VideoParameterSetID = (payload[0] & 0xF0) >> 4 // 상위 4비트
	}
	
	return vps, nil
}

// ParseH265SPS SPS NALU에서 시퀀스 파라미터 추출
func ParseH265SPS(naluData []byte) (*H265SPS, error) {
	if len(naluData) < 3 {
		return nil, ErrInvalidNALU
	}
	
	// NALU 타입 확인
	naluType := NALUType((naluData[0] & 0x7E) >> 1)
	if naluType != H265NALUTypeSPS {
		return nil, ErrInvalidNALU
	}
	
	sps := &H265SPS{}
	
	// 간단한 SPS 파싱 (실제 구현에서는 Exponential-Golomb 디코딩 필요)
	sps.Width = 1920  // 기본값
	sps.Height = 1080 // 기본값
	
	return sps, nil
}

// ParseH265PPS PPS NALU에서 픽처 파라미터 추출
func ParseH265PPS(naluData []byte) (*H265PPS, error) {
	if len(naluData) < 3 {
		return nil, ErrInvalidNALU
	}
	
	// NALU 타입 확인
	naluType := NALUType((naluData[0] & 0x7E) >> 1)
	if naluType != H265NALUTypePPS {
		return nil, ErrInvalidNALU
	}
	
	pps := &H265PPS{}
	
	// 기본적인 PPS 파싱
	return pps, nil
}

// IsH265KeyFrame H.265 NALU들에서 키프레임 여부 확인
func IsH265KeyFrame(nalus []NALU) bool {
	for _, nalu := range nalus {
		if nalu.Type.IsH265KeyFrame() {
			return true
		}
	}
	return false
}

// ExtractH265Config H.265 NALUs에서 설정 정보 (VPS/SPS/PPS) 추출
func ExtractH265Config(nalus []NALU) *CodecData {
	codecData := &CodecData{
		Type: StreamTypeH265,
	}
	
	for _, nalu := range nalus {
		switch nalu.Type {
		case H265NALUTypeVPS:
			if len(nalu.Data) > 0 {
				codecData.VPS = nalu.Data
			}
		case H265NALUTypeSPS:
			if len(nalu.Data) > 0 {
				codecData.SPS = nalu.Data
				
				// SPS 파싱하여 추가 정보 추출
				if sps, err := ParseH265SPS(nalu.Data); err == nil {
					codecData.Width = sps.Width
					codecData.Height = sps.Height
				}
			}
		case H265NALUTypePPS:
			if len(nalu.Data) > 0 {
				codecData.PPS = nalu.Data
			}
		}
	}
	
	return codecData
}

// SeparateH265NALUs 설정 NALUs와 프레임 NALUs 분리
func SeparateH265NALUs(nalus []NALU) (config []NALU, frame []NALU) {
	for _, nalu := range nalus {
		if nalu.IsConfig {
			config = append(config, nalu)
		} else {
			frame = append(frame, nalu)
		}
	}
	return
}

// GetH265FrameType NALU들을 기반으로 프레임 타입 결정
func GetH265FrameType(nalus []NALU) (isKey bool, isConfig bool) {
	for _, nalu := range nalus {
		if nalu.IsKey {
			isKey = true
		}
		if nalu.IsConfig {
			isConfig = true
		}
	}
	return
}

// CreateH265AnnexBFormat H.265 NALUs를 Annex-B 형식으로 변환
func CreateH265AnnexBFormat(nalus []NALU) []byte {
	// H.264와 동일한 방식 사용
	return CreateAnnexBFormat(nalus)
}

// IsH265RandomAccessPoint 랜덤 액세스 포인트(RAP) 여부 확인
func IsH265RandomAccessPoint(nalus []NALU) bool {
	for _, nalu := range nalus {
		naluType := nalu.Type
		// BLA, CRA, IDR 타입들은 모두 RAP
		if naluType >= H265NALUTypeSliceBLA_W_LP && naluType <= H265NALUTypeSliceCRA_NUT {
			return true
		}
	}
	return false
}