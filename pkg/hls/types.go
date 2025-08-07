package hls

import (
	"time"
)

// HLS 관련 상수 정의
const (
	// 기본 설정값
	DefaultSegmentDuration = 6 * time.Second   // 기본 세그먼트 길이
	DefaultPlaylistSize    = 10                // 플레이리스트 세그먼트 수
	DefaultPort           = 8081               // 기본 HLS 서버 포트
	
	// 세그먼트 형식
	SegmentFormatTS   SegmentFormat = "ts"    // MPEG-TS 형식
	SegmentFormatFMP4 SegmentFormat = "fmp4"  // fragmented MP4 형식
	
	// HLS 표준 값들
	HLSVersion        = 3                     // HLS 프로토콜 버전
	TargetDuration    = 10                    // #EXT-X-TARGETDURATION 값
	MaxSegmentAge     = 60 * time.Second      // 세그먼트 최대 보존 시간
)

// SegmentFormat HLS 세그먼트 형식
type SegmentFormat string

// HLSConfig HLS 서버 설정
type HLSConfig struct {
	Port             int               `yaml:"port"`              // HTTP 서버 포트
	SegmentDuration  time.Duration     `yaml:"segment_duration"`  // 세그먼트 길이
	PlaylistSize     int               `yaml:"playlist_size"`     // 플레이리스트 세그먼트 수
	Format           SegmentFormat     `yaml:"format"`            // 세그먼트 형식 (ts/fmp4)
	CORSEnabled      bool              `yaml:"cors_enabled"`      // CORS 지원 여부
	LowLatency       bool              `yaml:"low_latency"`       // 저지연 모드
	StoragePath      string            `yaml:"storage_path"`      // 세그먼트 저장 경로 (빈 문자열이면 메모리)
	MaxConcurrent    int               `yaml:"max_concurrent"`    // 최대 동시 스트림 수
}

// GetDefaultConfig 기본 HLS 설정 반환
func GetDefaultConfig() HLSConfig {
	return HLSConfig{
		Port:             DefaultPort,
		SegmentDuration:  DefaultSegmentDuration,
		PlaylistSize:     DefaultPlaylistSize,
		Format:           SegmentFormatTS,
		CORSEnabled:      true,
		LowLatency:       false,
		StoragePath:      "", // 메모리 저장
		MaxConcurrent:    100,
	}
}

// StreamInfo HLS 스트림 정보
type StreamInfo struct {
	StreamID    string        `json:"stream_id"`    // 스트림 식별자
	StartTime   time.Time     `json:"start_time"`   // 스트림 시작 시간
	LastUpdate  time.Time     `json:"last_update"`  // 마지막 업데이트 시간
	SegmentCount int          `json:"segment_count"` // 총 세그먼트 수
	Bitrate     int           `json:"bitrate"`      // 추정 비트레이트
	Resolution  string        `json:"resolution"`   // 해상도 (예: "1280x720")
	Codec       string        `json:"codec"`        // 코덱 정보
	IsActive    bool          `json:"is_active"`    // 활성 상태
}

// SegmentInfo 개별 세그먼트 정보
type SegmentInfo struct {
	Index      int           `json:"index"`       // 세그먼트 인덱스
	Duration   time.Duration `json:"duration"`    // 세그먼트 길이
	Size       int64         `json:"size"`        // 파일 크기
	Timestamp  time.Time     `json:"timestamp"`   // 생성 시간
	URL        string        `json:"url"`         // 세그먼트 URL
	IsKeyFrame bool          `json:"is_keyframe"` // 키프레임으로 시작하는지 여부
}

// PlaylistType 플레이리스트 타입
type PlaylistType int

const (
	PlaylistTypeLive PlaylistType = iota // 라이브 스트림
	PlaylistTypeVOD                      // VOD (Video On Demand)
	PlaylistTypeEvent                    // 이벤트 스트림
)

// String PlaylistType을 문자열로 변환
func (pt PlaylistType) String() string {
	switch pt {
	case PlaylistTypeLive:
		return "LIVE"
	case PlaylistTypeVOD:
		return "VOD"
	case PlaylistTypeEvent:
		return "EVENT"
	default:
		return "UNKNOWN"
	}
}