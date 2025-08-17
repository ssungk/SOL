package hls

import (
	"fmt"
	"sol/pkg/media"
	"time"
)

// Segment HLS 세그먼트 인터페이스
type Segment interface {
	// GetIndex 세그먼트 인덱스 반환
	GetIndex() int
	
	// GetDuration 세그먼트 길이 반환
	GetDuration() time.Duration
	
	// GetData 세그먼트 바이너리 데이터 반환
	GetData() ([]byte, error)
	
	// GetURL 세그먼트 URL 반환
	GetURL() string
	
	// GetTimestamp 세그먼트 생성 시간 반환
	GetTimestamp() time.Time
	
	// IsReady 세그먼트가 완성되어 제공 가능한지 확인
	IsReady() bool
	
	// GetSize 세그먼트 크기 반환
	GetSize() int64
	
	// StartsWithKeyFrame 키프레임으로 시작하는지 확인
	StartsWithKeyFrame() bool
	
	// Release 세그먼트 리소스 해제
	Release()
}

// BaseSegment 기본 세그먼트 구현
type BaseSegment struct {
	index         int           // 세그먼트 인덱스
	duration      time.Duration // 세그먼트 길이
	timestamp     time.Time     // 생성 시간
	streamID      string        // 스트림 ID
	startsWithKey bool          // 키프레임 시작 여부
	ready         bool          // 완성 상태
	data          []byte        // 세그먼트 데이터
}

// NewBaseSegment 기본 세그먼트 생성
func NewBaseSegment(index int, streamID string, duration time.Duration) *BaseSegment {
	return &BaseSegment{
		index:     index,
		streamID:  streamID,
		duration:  duration,
		timestamp: time.Now(),
		ready:     false,
	}
}

// GetIndex implements Segment interface
func (s *BaseSegment) GetIndex() int {
	return s.index
}

// GetDuration implements Segment interface
func (s *BaseSegment) GetDuration() time.Duration {
	return s.duration
}

// GetData implements Segment interface
func (s *BaseSegment) GetData() ([]byte, error) {
	if !s.ready {
		return nil, fmt.Errorf("segment %d not ready", s.index)
	}
	return s.data, nil
}

// GetURL implements Segment interface
func (s *BaseSegment) GetURL() string {
	return fmt.Sprintf("seg%d.ts", s.index)
}

// GetTimestamp implements Segment interface
func (s *BaseSegment) GetTimestamp() time.Time {
	return s.timestamp
}

// IsReady implements Segment interface
func (s *BaseSegment) IsReady() bool {
	return s.ready
}

// GetSize implements Segment interface
func (s *BaseSegment) GetSize() int64 {
	return int64(len(s.data))
}

// StartsWithKeyFrame implements Segment interface
func (s *BaseSegment) StartsWithKeyFrame() bool {
	return s.startsWithKey
}

// Release implements Segment interface
func (s *BaseSegment) Release() {
	s.data = nil
	s.ready = false
}

// SetData 세그먼트 데이터 설정
func (s *BaseSegment) SetData(data []byte) {
	s.data = make([]byte, len(data))
	copy(s.data, data)
	s.ready = true
}

// SetKeyFrameStart 키프레임 시작 설정
func (s *BaseSegment) SetKeyFrameStart(starts bool) {
	s.startsWithKey = starts
}

// SegmentBuilder 세그먼트 빌더 인터페이스
type SegmentBuilder interface {
	// StartSegment 새 세그먼트 시작
	StartSegment(index int, streamID string) error
	
	// AddFrame 프레임을 현재 세그먼트에 추가
	AddFrame(frame media.MediaFrame) error
	
	// FinishSegment 현재 세그먼트 완성
	FinishSegment() (Segment, error)
	
	// Reset 빌더 상태 초기화
	Reset()
	
	// GetCurrentDuration 현재 세그먼트의 길이 반환
	GetCurrentDuration() time.Duration
	
	// CanFinish 세그먼트 완성 가능 여부 확인
	CanFinish() bool
}

// SegmentStore 세그먼트 저장소 인터페이스
type SegmentStore interface {
	// AddSegment 세그먼트 추가
	AddSegment(streamID string, segment Segment) error
	
	// GetSegment 세그먼트 조회
	GetSegment(streamID string, index int) (Segment, error)
	
	// RemoveOldSegments 오래된 세그먼트 제거
	RemoveOldSegments(streamID string, keepCount int) error
	
	// GetSegmentList 세그먼트 목록 조회
	GetSegmentList(streamID string) ([]Segment, error)
	
	// Clear 모든 세그먼트 제거
	Clear(streamID string) error
	
	// GetStats 저장소 통계 정보
	GetStats(streamID string) (map[string]any, error)
}

// MemorySegmentStore 메모리 기반 세그먼트 저장소
type MemorySegmentStore struct {
	segments map[string][]Segment // streamID -> segments
}

// NewMemorySegmentStore 메모리 저장소 생성
func NewMemorySegmentStore() *MemorySegmentStore {
	return &MemorySegmentStore{
		segments: make(map[string][]Segment),
	}
}

// AddSegment implements SegmentStore interface
func (store *MemorySegmentStore) AddSegment(streamID string, segment Segment) error {
	if store.segments[streamID] == nil {
		store.segments[streamID] = make([]Segment, 0)
	}
	store.segments[streamID] = append(store.segments[streamID], segment)
	return nil
}

// GetSegment implements SegmentStore interface
func (store *MemorySegmentStore) GetSegment(streamID string, index int) (Segment, error) {
	segments, exists := store.segments[streamID]
	if !exists {
		return nil, fmt.Errorf("stream %s not found", streamID)
	}
	
	for _, segment := range segments {
		if segment.GetIndex() == index {
			return segment, nil
		}
	}
	
	return nil, fmt.Errorf("segment %d not found in stream %s", index, streamID)
}

// RemoveOldSegments implements SegmentStore interface
func (store *MemorySegmentStore) RemoveOldSegments(streamID string, keepCount int) error {
	segments, exists := store.segments[streamID]
	if !exists {
		return nil
	}
	
	if len(segments) <= keepCount {
		return nil
	}
	
	// Release old segments
	removeCount := len(segments) - keepCount
	for i := 0; i < removeCount; i++ {
		segments[i].Release()
	}
	
	// Keep only recent segments
	store.segments[streamID] = segments[removeCount:]
	return nil
}

// GetSegmentList implements SegmentStore interface
func (store *MemorySegmentStore) GetSegmentList(streamID string) ([]Segment, error) {
	segments, exists := store.segments[streamID]
	if !exists {
		return []Segment{}, nil
	}
	
	// Return copy to avoid modification
	result := make([]Segment, len(segments))
	copy(result, segments)
	return result, nil
}

// Clear implements SegmentStore interface
func (store *MemorySegmentStore) Clear(streamID string) error {
	segments, exists := store.segments[streamID]
	if !exists {
		return nil
	}
	
	// Release all segments
	for _, segment := range segments {
		segment.Release()
	}
	
	delete(store.segments, streamID)
	return nil
}

// GetStats implements SegmentStore interface
func (store *MemorySegmentStore) GetStats(streamID string) (map[string]any, error) {
	segments, exists := store.segments[streamID]
	if !exists {
		return map[string]any{
			"segment_count": 0,
			"total_size":    0,
		}, nil
	}
	
	totalSize := int64(0)
	for _, segment := range segments {
		totalSize += segment.GetSize()
	}
	
	return map[string]any{
		"segment_count": len(segments),
		"total_size":    totalSize,
	}, nil
}