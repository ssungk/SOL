package hls

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Playlist HLS 플레이리스트 관리
type Playlist struct {
	streamID       string        // 스트림 ID
	playlistType   PlaylistType  // 플레이리스트 타입
	targetDuration int           // 타겟 지속시간 (초)
	version        int           // HLS 버전
	mediaSequence  int           // 미디어 시퀀스 번호
	segments       []Segment     // 세그먼트 목록
	maxSegments    int           // 최대 세그먼트 수
	format         SegmentFormat // 세그먼트 형식
	allowCache     bool          // 캐시 허용 여부
	endList        bool          // 종료 목록 여부
}

// NewPlaylist 새 플레이리스트 생성
func NewPlaylist(streamID string, config HLSConfig) *Playlist {
	return &Playlist{
		streamID:       streamID,
		playlistType:   PlaylistTypeLive,
		targetDuration: int(config.SegmentDuration.Seconds()) + 1, // 여유값 추가
		version:        HLSVersion,
		mediaSequence:  0,
		segments:       make([]Segment, 0),
		maxSegments:    config.PlaylistSize,
		format:         config.Format,
		allowCache:     false, // 라이브는 캐시 비허용
		endList:        false,
	}
}

// AddSegment 플레이리스트에 세그먼트 추가
func (p *Playlist) AddSegment(segment Segment) {
	if segment == nil || !segment.IsReady() {
		slog.Warn("Cannot add invalid or unready segment", "streamID", p.streamID)
		return
	}
	
	// 세그먼트 추가
	p.segments = append(p.segments, segment)
	
	// 최대 세그먼트 수 제한
	if len(p.segments) > p.maxSegments {
		// 오래된 세그먼트 제거
		oldSegment := p.segments[0]
		oldSegment.Release()
		p.segments = p.segments[1:]
		p.mediaSequence++
	}
	
	slog.Debug("Segment added to playlist", 
		"streamID", p.streamID, 
		"segmentIndex", segment.GetIndex(),
		"playlistSize", len(p.segments))
}

// RemoveOldSegments 오래된 세그먼트 제거
func (p *Playlist) RemoveOldSegments(keepCount int) {
	if len(p.segments) <= keepCount {
		return
	}
	
	removeCount := len(p.segments) - keepCount
	for i := 0; i < removeCount; i++ {
		p.segments[i].Release()
		p.mediaSequence++
	}
	
	p.segments = p.segments[removeCount:]
	
	slog.Debug("Old segments removed from playlist",
		"streamID", p.streamID,
		"removedCount", removeCount,
		"remainingCount", len(p.segments))
}

// GenerateMediaPlaylist 미디어 플레이리스트 생성 (M3U8)
func (p *Playlist) GenerateMediaPlaylist() []byte {
	var builder strings.Builder
	
	// 헤더
	builder.WriteString("#EXTM3U\n")
	builder.WriteString(fmt.Sprintf("#EXT-X-VERSION:%d\n", p.version))
	builder.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", p.targetDuration))
	builder.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", p.mediaSequence))
	
	// 캐시 허용 여부
	if !p.allowCache {
		builder.WriteString("#EXT-X-ALLOW-CACHE:NO\n")
	}
	
	// 플레이리스트 타입
	switch p.playlistType {
	case PlaylistTypeVOD:
		builder.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	case PlaylistTypeEvent:
		builder.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	}
	
	// 세그먼트 목록
	for _, segment := range p.segments {
		duration := segment.GetDuration().Seconds()
		builder.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", duration))
		builder.WriteString(segment.GetURL())
		builder.WriteString("\n")
	}
	
	// VOD인 경우 종료 태그 추가
	if p.endList || p.playlistType == PlaylistTypeVOD {
		builder.WriteString("#EXT-X-ENDLIST\n")
	}
	
	return []byte(builder.String())
}

// GenerateMasterPlaylist 마스터 플레이리스트 생성 (다중 품질용)
func (p *Playlist) GenerateMasterPlaylist(variants []PlaylistVariant) []byte {
	var builder strings.Builder
	
	builder.WriteString("#EXTM3U\n")
	builder.WriteString(fmt.Sprintf("#EXT-X-VERSION:%d\n", p.version))
	
	// 각 품질별 스트림 정보
	for _, variant := range variants {
		builder.WriteString("#EXT-X-STREAM-INF:")
		
		// 대역폭 정보
		builder.WriteString(fmt.Sprintf("BANDWIDTH=%d", variant.Bandwidth))
		
		// 해상도 정보 (옵션)
		if variant.Resolution != "" {
			builder.WriteString(fmt.Sprintf(",RESOLUTION=%s", variant.Resolution))
		}
		
		// 코덱 정보 (옵션)
		if variant.Codecs != "" {
			builder.WriteString(fmt.Sprintf(",CODECS=\"%s\"", variant.Codecs))
		}
		
		builder.WriteString("\n")
		builder.WriteString(variant.URL)
		builder.WriteString("\n")
	}
	
	return []byte(builder.String())
}

// PlaylistVariant 플레이리스트 변형 정보 (다중 품질용)
type PlaylistVariant struct {
	Bandwidth  int    `json:"bandwidth"`  // 대역폭 (bps)
	Resolution string `json:"resolution"` // 해상도 (예: "1280x720")
	Codecs     string `json:"codecs"`     // 코덱 정보 (예: "avc1.64001f,mp4a.40.2")
	URL        string `json:"url"`        // 플레이리스트 URL
}

// GetSegmentCount 현재 세그먼트 수 반환
func (p *Playlist) GetSegmentCount() int {
	return len(p.segments)
}

// GetCurrentSequence 현재 미디어 시퀀스 반환
func (p *Playlist) GetCurrentSequence() int {
	return p.mediaSequence
}

// GetDuration 전체 플레이리스트 길이 반환
func (p *Playlist) GetDuration() time.Duration {
	var total time.Duration
	for _, segment := range p.segments {
		total += segment.GetDuration()
	}
	return total
}

// GetSegments 세그먼트 목록 반환 (복사본)
func (p *Playlist) GetSegments() []Segment {
	result := make([]Segment, len(p.segments))
	copy(result, p.segments)
	return result
}

// FindSegment 인덱스로 세그먼트 찾기
func (p *Playlist) FindSegment(index int) (Segment, bool) {
	for _, segment := range p.segments {
		if segment.GetIndex() == index {
			return segment, true
		}
	}
	return nil, false
}

// SetEndList 종료 목록으로 설정
func (p *Playlist) SetEndList(endList bool) {
	p.endList = endList
	if endList {
		p.playlistType = PlaylistTypeVOD
	}
}

// IsLive 라이브 스트림 여부 확인
func (p *Playlist) IsLive() bool {
	return p.playlistType == PlaylistTypeLive && !p.endList
}

// Clear 모든 세그먼트 제거
func (p *Playlist) Clear() {
	for _, segment := range p.segments {
		segment.Release()
	}
	p.segments = p.segments[:0]
	p.mediaSequence = 0
	
	slog.Debug("Playlist cleared", "streamID", p.streamID)
}

// GetStats 플레이리스트 통계 정보 반환
func (p *Playlist) GetStats() map[string]any {
	totalSize := int64(0)
	totalDuration := time.Duration(0)
	keyFrameCount := 0
	
	for _, segment := range p.segments {
		totalSize += segment.GetSize()
		totalDuration += segment.GetDuration()
		if segment.StartsWithKeyFrame() {
			keyFrameCount++
		}
	}
	
	return map[string]any{
		"stream_id":        p.streamID,
		"playlist_type":    p.playlistType.String(),
		"segment_count":    len(p.segments),
		"media_sequence":   p.mediaSequence,
		"total_duration":   totalDuration.Seconds(),
		"total_size":       totalSize,
		"keyframe_count":   keyFrameCount,
		"target_duration":  p.targetDuration,
		"is_live":          p.IsLive(),
		"format":           string(p.format),
	}
}

// SortSegmentsByIndex 세그먼트를 인덱스 순으로 정렬
func (p *Playlist) SortSegmentsByIndex() {
	sort.Slice(p.segments, func(i, j int) bool {
		return p.segments[i].GetIndex() < p.segments[j].GetIndex()
	})
}

// Validate 플레이리스트 유효성 검증
func (p *Playlist) Validate() error {
	if p.streamID == "" {
		return fmt.Errorf("stream ID cannot be empty")
	}
	
	if p.targetDuration <= 0 {
		return fmt.Errorf("target duration must be positive")
	}
	
	if p.maxSegments <= 0 {
		return fmt.Errorf("max segments must be positive")
	}
	
	// 세그먼트 인덱스 중복 검사
	indexes := make(map[int]bool)
	for _, segment := range p.segments {
		index := segment.GetIndex()
		if indexes[index] {
			return fmt.Errorf("duplicate segment index: %d", index)
		}
		indexes[index] = true
	}
	
	return nil
}