package hls

import (
	"fmt"
	"log/slog"
	"sol/pkg/hls/ts"
	"sol/pkg/media"
	"sync"
	"time"
	"unsafe"
)

// Packager HLS 패키징 처리기 (MediaSink 구현)
type Packager struct {
	streamID     string          // 스트림 ID
	config       HLSConfig       // 설정
	segmentStore SegmentStore    // 세그먼트 저장소
	playlist     *Playlist       // 플레이리스트
	
	// 세그먼트 빌더
	currentBuilder SegmentBuilder // 현재 세그먼트 빌더
	segmentIndex   int            // 세그먼트 인덱스
	
	// 프레임 버퍼링
	frameBuffer    []media.Frame  // 프레임 버퍼
	lastKeyFrame   time.Time      // 마지막 키프레임 시간
	segmentStart   time.Time      // 현재 세그먼트 시작 시간
	
	// 상태 관리
	isActive       bool           // 활성 상태
	mutex          sync.RWMutex   // 동시성 제어
	
	// 통계
	totalFrames    int            // 총 프레임 수
	totalSegments  int            // 총 세그먼트 수
	lastFrameTime  time.Time      // 마지막 프레임 시간
}

// NewPackager 새 패키저 생성
func NewPackager(streamID string, config HLSConfig, store SegmentStore, playlist *Playlist) *Packager {
	packager := &Packager{
		streamID:     streamID,
		config:       config,
		segmentStore: store,
		playlist:     playlist,
		segmentIndex: 0,
		frameBuffer:  make([]media.Frame, 0),
		isActive:     true,
	}
	
	// 세그먼트 빌더 생성 (TS 형식으로 시작)
	packager.currentBuilder = NewTSSegmentBuilder()
	
	return packager
}

// MediaSink 인터페이스 구현

// ID implements MediaSink interface
func (p *Packager) ID() uintptr {
	return uintptr(unsafe.Pointer(p))
}

// NodeType implements MediaSink interface
func (p *Packager) NodeType() media.NodeType {
	return media.NodeTypeHLS
}

// Address implements MediaSink interface
func (p *Packager) Address() string {
	return fmt.Sprintf("hls://%s", p.streamID)
}

// Close implements MediaSink interface
func (p *Packager) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	p.isActive = false
	
	// 현재 세그먼트가 있으면 완성
	if p.currentBuilder.CanFinish() {
		if segment, err := p.currentBuilder.FinishSegment(); err == nil {
			p.finalizeSegment(segment)
		}
	}
	
	slog.Info("HLS packager stopped", "streamID", p.streamID, "totalSegments", p.totalSegments)
	return nil
}

// SendMediaFrame implements MediaSink interface
func (p *Packager) SendMediaFrame(streamId string, frame media.Frame) error {
	if !p.isActive {
		return fmt.Errorf("packager not active")
	}
	
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	p.totalFrames++
	p.lastFrameTime = time.Now()
	
	// 키프레임 감지
	isKeyFrame := media.IsVideoKeyFrame(frame.SubType)
	if isKeyFrame && frame.Type == media.TypeVideo {
		p.lastKeyFrame = time.Now()
	}
	
	// 현재 세그먼트에 프레임 추가
	if err := p.currentBuilder.AddFrame(frame); err != nil {
		slog.Error("Failed to add frame to segment", "streamID", p.streamID, "err", err)
		return err
	}
	
	// 세그먼트 완성 조건 확인
	if p.shouldFinishSegment(isKeyFrame) {
		if err := p.finishCurrentSegment(); err != nil {
			slog.Error("Failed to finish segment", "streamID", p.streamID, "err", err)
			return err
		}
		
		// 새 세그먼트 시작
		if err := p.startNextSegment(); err != nil {
			slog.Error("Failed to start next segment", "streamID", p.streamID, "err", err)
			return err
		}
	}
	
	slog.Debug("Frame added to HLS packager", 
		"streamID", p.streamID,
		"subType", frame.SubType,
		"timestamp", frame.Timestamp,
		"isKeyFrame", isKeyFrame)
	
	return nil
}

// SendMetadata implements MediaSink interface
func (p *Packager) SendMetadata(streamId string, metadata map[string]string) error {
	if !p.isActive {
		return nil
	}
	
	slog.Debug("Metadata received in HLS packager", "streamID", p.streamID, "metadata", metadata)
	// 메타데이터 처리는 필요시 구현
	return nil
}

// 세그먼트 관리 메서드들

// shouldFinishSegment 세그먼트 완성 여부 확인
func (p *Packager) shouldFinishSegment(isKeyFrame bool) bool {
	// 최소 세그먼트 길이 확인
	currentDuration := p.currentBuilder.GetCurrentDuration()
	if currentDuration < p.config.SegmentDuration {
		return false
	}
	
	// 키프레임에서 세그먼트 분할 (GOP 경계)
	if isKeyFrame && currentDuration >= p.config.SegmentDuration {
		return true
	}
	
	// 최대 길이 초과 시 강제 분할
	maxDuration := p.config.SegmentDuration * 2
	if currentDuration >= maxDuration {
		return true
	}
	
	return false
}

// finishCurrentSegment 현재 세그먼트 완성
func (p *Packager) finishCurrentSegment() error {
	if !p.currentBuilder.CanFinish() {
		return fmt.Errorf("cannot finish current segment")
	}
	
	segment, err := p.currentBuilder.FinishSegment()
	if err != nil {
		return fmt.Errorf("failed to finish segment: %w", err)
	}
	
	return p.finalizeSegment(segment)
}

// finalizeSegment 세그먼트 완료 처리
func (p *Packager) finalizeSegment(segment Segment) error {
	// 세그먼트 저장
	if err := p.segmentStore.AddSegment(p.streamID, segment); err != nil {
		return fmt.Errorf("failed to store segment: %w", err)
	}
	
	// 플레이리스트에 추가
	p.playlist.AddSegment(segment)
	
	p.totalSegments++
	
	slog.Info("HLS segment completed",
		"streamID", p.streamID,
		"segmentIndex", segment.GetIndex(),
		"duration", segment.GetDuration().Seconds(),
		"size", segment.GetSize(),
		"totalSegments", p.totalSegments)
	
	return nil
}

// startNextSegment 다음 세그먼트 시작
func (p *Packager) startNextSegment() error {
	p.segmentIndex++
	p.segmentStart = time.Now()
	
	// 빌더 리셋 및 새 세그먼트 시작
	p.currentBuilder.Reset()
	if err := p.currentBuilder.StartSegment(p.segmentIndex, p.streamID); err != nil {
		return fmt.Errorf("failed to start segment %d: %w", p.segmentIndex, err)
	}
	
	return nil
}

// GetStats 패키저 통계 반환
func (p *Packager) GetStats() map[string]any {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	return map[string]any{
		"stream_id":       p.streamID,
		"is_active":       p.isActive,
		"total_frames":    p.totalFrames,
		"total_segments":  p.totalSegments,
		"current_segment": p.segmentIndex,
		"last_frame_time": p.lastFrameTime,
		"format":          string(p.config.Format),
	}
}

// SegmentBuilder 인터페이스 구현들

// TSSegmentBuilder TS 세그먼트 빌더
type TSSegmentBuilder struct {
	segment       *BaseSegment  // 현재 세그먼트
	frames        []media.Frame // 프레임 버퍼
	startTime     time.Time     // 시작 시간
	hasKeyFrame   bool          // 키프레임 보유 여부
}

// NewTSSegmentBuilder TS 세그먼트 빌더 생성
func NewTSSegmentBuilder() *TSSegmentBuilder {
	return &TSSegmentBuilder{
		frames: make([]media.Frame, 0),
	}
}

// StartSegment implements SegmentBuilder interface
func (b *TSSegmentBuilder) StartSegment(index int, streamID string) error {
	b.segment = NewBaseSegment(index, streamID, 0)
	b.frames = b.frames[:0] // 슬라이스 재사용
	b.startTime = time.Now()
	b.hasKeyFrame = false
	
	return nil
}

// AddFrame implements SegmentBuilder interface
func (b *TSSegmentBuilder) AddFrame(frame media.Frame) error {
	if b.segment == nil {
		return fmt.Errorf("segment not started")
	}
	
	// 키프레임 감지
	if media.IsVideoKeyFrame(frame.SubType) && frame.Type == media.TypeVideo {
		if !b.hasKeyFrame {
			b.segment.SetKeyFrameStart(true)
			b.hasKeyFrame = true
		}
	}
	
	b.frames = append(b.frames, frame)
	return nil
}

// FinishSegment implements SegmentBuilder interface
func (b *TSSegmentBuilder) FinishSegment() (Segment, error) {
	if b.segment == nil || len(b.frames) == 0 {
		return nil, fmt.Errorf("no frames to finish segment")
	}
	
	// 세그먼트 길이 계산
	duration := time.Since(b.startTime)
	b.segment.duration = duration
	
	// TS 데이터 생성 (현재는 더미 데이터)
	// MPEG-TS 패킷 생성 로직 추후 구현
	tsData := b.generateTSData()
	b.segment.SetData(tsData)
	
	return b.segment, nil
}

// Reset implements SegmentBuilder interface
func (b *TSSegmentBuilder) Reset() {
	b.segment = nil
	b.frames = b.frames[:0]
	b.hasKeyFrame = false
}

// GetCurrentDuration implements SegmentBuilder interface
func (b *TSSegmentBuilder) GetCurrentDuration() time.Duration {
	if b.startTime.IsZero() {
		return 0
	}
	return time.Since(b.startTime)
}

// CanFinish implements SegmentBuilder interface
func (b *TSSegmentBuilder) CanFinish() bool {
	return b.segment != nil && len(b.frames) > 0
}

// generateTSData TS 데이터 생성 (실제 MPEG-TS 구현)
func (b *TSSegmentBuilder) generateTSData() []byte {
	// TS Writer를 사용하여 실제 MPEG-TS 패킷 생성
	writer := ts.NewTSWriter()
	
	tsData, err := writer.WriteSegment(b.frames)
	if err != nil {
		slog.Error("Failed to generate TS data", "err", err)
		// 오류 시 빈 데이터 반환
		return make([]byte, 0)
	}
	
	return tsData
}

// --- MediaSink 인터페이스 구현 ---

// SubscribedStreams MediaSink 인터페이스 구현 - 구독 중인 스트림 ID 목록 반환
func (p *Packager) SubscribedStreams() []string {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	if p.isActive {
		return []string{p.streamID}
	}
	return nil
}

// PreferredFormat MediaSink 인터페이스 구현 - HLS는 MPEG-TS용 StartCode 포맷 선호
func (p *Packager) PreferredFormat(codecType media.CodecType) media.FormatType {
	switch codecType {
	case media.CodecH264, media.CodecH265:
		return media.FormatAnnexB // MPEG-TS는 StartCode 포맷 사용
	case media.CodecAAC:
		return media.FormatADTS // AAC는 ADTS 포맷 선호
	default:
		return media.FormatRaw
	}
}