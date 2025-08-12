package codec

import (
	"bytes"
)

// H.264 시작 코드들
var (
	StartCode4 = []byte{0x00, 0x00, 0x00, 0x01} // 4바이트 시작 코드
	StartCode3 = []byte{0x00, 0x00, 0x01}       // 3바이트 시작 코드
)

// SPS (Sequence Parameter Set) 정보
type H264SPS struct {
	ProfileIDC                uint8  // 프로파일 IDC
	LevelIDC                  uint8  // 레벨 IDC
	SeqParameterSetID         uint8  // SPS ID
	ChromaFormatIDC           uint8  // 크로마 포맷
	BitDepthLuma              uint8  // 루마 비트 깊이
	BitDepthChroma            uint8  // 크로마 비트 깊이
	PicWidthInMbsMinus1       uint32 // 매크로블록 단위 너비 - 1
	PicHeightInMapUnitsMinus1 uint32 // 매크로블록 단위 높이 - 1
	Width                     uint16 // 실제 너비 (픽셀)
	Height                    uint16 // 실제 높이 (픽셀)
	FrameMbsOnlyFlag          bool   // 프레임만 사용하는지 여부
}

// PPS (Picture Parameter Set) 정보
type H264PPS struct {
	PicParameterSetID     uint8 // PPS ID
	SeqParameterSetID     uint8 // 참조하는 SPS ID
	EntropyCodingModeFlag bool  // 엔트로피 코딩 모드
}

// ParseH264NALUs H.264 스트림에서 NALU들을 파싱
func ParseH264NALUs(data []byte) ([]NALU, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var nalus []NALU
	
	// 시작 코드 기반으로 NALU 분리
	positions := findStartCodes(data)
	
	for i, pos := range positions {
		var naluData []byte
		
		if i+1 < len(positions) {
			// 다음 시작 코드까지
			naluData = data[pos:positions[i+1]]
		} else {
			// 마지막 NALU
			naluData = data[pos:]
		}
		
		// 시작 코드 제거
		startCodeLen := getStartCodeLength(naluData)
		if startCodeLen == 0 || len(naluData) <= startCodeLen {
			continue
		}
		
		naluPayload := naluData[startCodeLen:]
		if len(naluPayload) == 0 {
			continue
		}
		
		// NALU 헤더 파싱
		naluHeader := naluPayload[0]
		forbiddenBit := (naluHeader & 0x80) != 0
		if forbiddenBit {
			continue // 금지 비트가 설정된 경우 건너뛰기
		}
		
		nalRefIDC := (naluHeader & 0x60) >> 5
		naluType := NALUType(naluHeader & 0x1F)
		
		// NALU 생성
		nalu := NALU{
			Type:     naluType,
			Data:     naluPayload, // 시작 코드 제외한 전체 데이터
			IsConfig: naluType.IsH264Config(),
			IsKey:    naluType.IsH264KeyFrame(),
		}
		
		// 참조 프레임 여부도 키프레임 판별에 추가
		if nalRefIDC > 0 && (naluType == H264NALUTypeSlice || naluType == H264NALUTypeSliceA) {
			// 슬라이스가 참조 프레임인 경우 키프레임으로 간주할 수 있음
			// 하지만 정확한 판별을 위해서는 슬라이스 헤더를 더 파싱해야 함
		}
		
		nalus = append(nalus, nalu)
	}
	
	return nalus, nil
}

// findStartCodes 데이터에서 모든 시작 코드 위치 찾기
func findStartCodes(data []byte) []int {
	var positions []int
	
	for i := 0; i < len(data)-2; i++ {
		// 3바이트 시작 코드 확인
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			positions = append(positions, i)
			i += 2 // 다음 위치로 점프
		} else if i < len(data)-3 {
			// 4바이트 시작 코드 확인
			if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
				positions = append(positions, i)
				i += 3 // 다음 위치로 점프
			}
		}
	}
	
	return positions
}

// getStartCodeLength 시작 코드 길이 반환 (3 또는 4바이트)
func getStartCodeLength(data []byte) int {
	if len(data) >= 4 && bytes.Equal(data[:4], StartCode4) {
		return 4
	}
	if len(data) >= 3 && bytes.Equal(data[:3], StartCode3) {
		return 3
	}
	return 0
}

// ParseH264SPS SPS NALU에서 시퀀스 파라미터 추출
func ParseH264SPS(naluData []byte) (*H264SPS, error) {
	if len(naluData) < 4 {
		return nil, ErrInvalidNALU
	}
	
	// NALU 헤더 확인
	naluType := NALUType(naluData[0] & 0x1F)
	if naluType != H264NALUTypeSPS {
		return nil, ErrInvalidNALU
	}
	
	sps := &H264SPS{}
	
	// 기본적인 SPS 파싱 (Exponential-Golomb 디코딩은 복잡하므로 핵심만 추출)
	sps.ProfileIDC = naluData[1]
	sps.LevelIDC = naluData[3]
	
	// 간단한 해상도 추출을 위한 휴리스틱
	// 실제 구현에서는 Exponential-Golomb 디코딩이 필요함
	sps.Width = 1920  // 기본값 (실제로는 SPS에서 파싱해야 함)
	sps.Height = 1080 // 기본값
	
	return sps, nil
}

// ParseH264PPS PPS NALU에서 픽처 파라미터 추출
func ParseH264PPS(naluData []byte) (*H264PPS, error) {
	if len(naluData) < 2 {
		return nil, ErrInvalidNALU
	}
	
	// NALU 헤더 확인
	naluType := NALUType(naluData[0] & 0x1F)
	if naluType != H264NALUTypePPS {
		return nil, ErrInvalidNALU
	}
	
	pps := &H264PPS{}
	
	// 기본적인 PPS 파싱 (실제로는 Exponential-Golomb 디코딩 필요)
	// 여기서는 간단한 구현만 제공
	
	return pps, nil
}

// IsH264KeyFrame H.264 NALU들에서 키프레임 여부 확인
func IsH264KeyFrame(nalus []NALU) bool {
	for _, nalu := range nalus {
		if nalu.Type == H264NALUTypeSliceIDR {
			return true
		}
	}
	return false
}

// ExtractH264Config H.264 NALUs에서 설정 정보 (SPS/PPS) 추출
func ExtractH264Config(nalus []NALU) *CodecData {
	codecData := &CodecData{
		Type: StreamTypeH264,
	}
	
	for _, nalu := range nalus {
		switch nalu.Type {
		case H264NALUTypeSPS:
			if len(nalu.Data) > 0 {
				codecData.SPS = nalu.Data
				
				// SPS 파싱하여 추가 정보 추출
				if sps, err := ParseH264SPS(nalu.Data); err == nil {
					codecData.Profile = sps.ProfileIDC
					codecData.Level = sps.LevelIDC
					codecData.Width = sps.Width
					codecData.Height = sps.Height
				}
			}
		case H264NALUTypePPS:
			if len(nalu.Data) > 0 {
				codecData.PPS = nalu.Data
			}
		}
	}
	
	return codecData
}

// CreateAnnexBFormat H.264 NALUs를 Annex-B 형식으로 변환 (시작 코드 추가)
func CreateAnnexBFormat(nalus []NALU) []byte {
	var result []byte
	
	for i, nalu := range nalus {
		// 첫 번째 NALU는 4바이트 시작 코드, 나머지는 3바이트 사용
		if i == 0 {
			result = append(result, StartCode4...)
		} else {
			result = append(result, StartCode3...)
		}
		result = append(result, nalu.Data...)
	}
	
	return result
}

// SeparateConfigAndFrameData 설정 NALUs와 프레임 NALUs 분리
func SeparateH264NALUs(nalus []NALU) (config []NALU, frame []NALU) {
	for _, nalu := range nalus {
		if nalu.IsConfig {
			config = append(config, nalu)
		} else {
			frame = append(frame, nalu)
		}
	}
	return
}

// GetH264FrameType NALU들을 기반으로 프레임 타입 결정
func GetH264FrameType(nalus []NALU) (isKey bool, isConfig bool) {
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