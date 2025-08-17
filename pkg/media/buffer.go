package media

import (
	"log/slog"
)

type StreamBuffer struct {
	// 시간순으로 정렬된 모든 프레임 (MediaFrame)
	frames []MediaFrame

	// 코덱 설정 데이터 (SPS/PPS, AudioSpecificConfig 등)
	extraData map[Codec]MediaFrame

	// 스트림 메타데이터 (width, height, framerate, audiocodecid 등)
	metadata map[string]string

	// 시간 기반 버퍼링 설정
	minBufferDurationMs uint32 // 최소 버퍼 시간 (ms) - 기본 2초
	maxBufferDurationMs uint32 // 최대 버퍼 시간 (ms) - 기본 10초
	maxFrames           int    // 안전장치로 최대 프레임 수

	// 키프레임 추적
	lastKeyFrameIndex int // 마지막 키프레임 위치
}

// 새로운 스트림 버퍼를 생성합니다 (기본 설정)
func NewStreamBuffer() *StreamBuffer {
	return NewStreamBufferWithConfig(2000, 10000, 1000) // 2-10초, 최대 1000프레임
}

// 설정 가능한 스트림 버퍼를 생성합니다
func NewStreamBufferWithConfig(minDurationMs, maxDurationMs uint32, maxFrames int) *StreamBuffer {
	return &StreamBuffer{
		frames:              make([]MediaFrame, 0),
		extraData:           make(map[Codec]MediaFrame),
		minBufferDurationMs: minDurationMs,
		maxBufferDurationMs: maxDurationMs,
		maxFrames:           maxFrames,
		lastKeyFrameIndex:   -1, // 아직 키프레임 없음
	}
}

// 프레임을 캐시에 추가합니다 (MediaFrame 사용)
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) AddFrame(frame MediaFrame) {
	// Handle extra data (config frames) separately
	if frame.Type == TypeConfig {
		sb.extraData[frame.Codec] = frame
		slog.Debug("Extra data cached", "codec", frame.Codec, "timestamp", frame.Timestamp)
		return
	}

	// 키프레임 추적 및 위치 기록
	if frame.IsKeyFrame() {
		sb.lastKeyFrameIndex = len(sb.frames)
		slog.Debug("New key frame detected", "timestamp", frame.Timestamp, "index", sb.lastKeyFrameIndex)
	}

	// Add frame to cache
	sb.frames = append(sb.frames, frame)

	// 시간 기준으로 오래된 프레임 정리
	sb.cleanupOldFrames()
}

// cleanupOldFrames 시간 기반으로 오래된 프레임을 정리합니다
func (sb *StreamBuffer) cleanupOldFrames() {
	if len(sb.frames) == 0 {
		return
	}

	currentTime := sb.getCurrentTimestamp() // 최신 프레임 타임스탬프
	minTime := currentTime - sb.minBufferDurationMs

	// 최소 버퍼 시간 이상 유지하면서 정리
	// 단, 키프레임은 보존하여 재생 연속성 확보
	cleanupIndex := 0
	for i, frame := range sb.frames {
		if frame.Timestamp >= minTime {
			break // 최소 시간 이후 프레임들은 모두 유지
		}

		// 키프레임이면서 최소 시간 내에 다른 키프레임이 있으면 제거 가능
		if frame.IsKeyFrame() {
			if sb.hasKeyFrameAfter(i, minTime) {
				cleanupIndex = i + 1
			}
		} else {
			cleanupIndex = i + 1
		}
	}

	// 정리 실행
	if cleanupIndex > 0 {
		sb.frames = sb.frames[cleanupIndex:]
		// 키프레임 인덱스 조정
		if sb.lastKeyFrameIndex >= cleanupIndex {
			sb.lastKeyFrameIndex -= cleanupIndex
		} else {
			sb.lastKeyFrameIndex = -1 // 키프레임이 정리됨
		}

		slog.Debug("Old frames cleaned up", "removedCount", cleanupIndex, "remainingCount", len(sb.frames))
	}

	// 안전장치: 최대 프레임 수 제한
	if len(sb.frames) > sb.maxFrames {
		excessCount := len(sb.frames) - sb.maxFrames
		sb.frames = sb.frames[excessCount:]
		// 키프레임 인덱스 조정
		if sb.lastKeyFrameIndex >= excessCount {
			sb.lastKeyFrameIndex -= excessCount
		} else {
			sb.lastKeyFrameIndex = -1
		}

		slog.Warn("Buffer overflow protection triggered", "removedCount", excessCount, "maxFrames", sb.maxFrames)
	}
}

// getCurrentTimestamp 현재(최신) 프레임의 타임스탬프 반환
func (sb *StreamBuffer) getCurrentTimestamp() uint32 {
	if len(sb.frames) == 0 {
		return 0
	}
	return sb.frames[len(sb.frames)-1].Timestamp
}

// hasKeyFrameAfter 지정된 인덱스 이후에 최소 시간 내 키프레임이 있는지 확인
func (sb *StreamBuffer) hasKeyFrameAfter(index int, minTime uint32) bool {
	for i := index + 1; i < len(sb.frames); i++ {
		frame := sb.frames[i]
		if frame.IsKeyFrame() && frame.Timestamp >= minTime {
			return true
		}
	}
	return false
}

// getBufferDuration 현재 버퍼의 총 지속 시간 반환 (ms)
func (sb *StreamBuffer) getBufferDuration() uint32 {
	if len(sb.frames) < 2 {
		return 0
	}
	return sb.frames[len(sb.frames)-1].Timestamp - sb.frames[0].Timestamp
}

// 메타데이터를 캐시에 추가합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) AddMetadata(metadata map[string]string) {
	// Make a copy of metadata to avoid reference issues
	cached := make(map[string]string)
	for k, v := range metadata {
		cached[k] = v
	}

	sb.metadata = cached
	slog.Debug("Metadata cached")
}

// 새로운 플레이어를 위해 적절한 순서로 모든 캐시된 프레임을 반환합니다
// H.264 디코딩을 위해 키프레임부터 시작하는 GOP를 보장합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) GetCachedFrames() []MediaFrame {
	allFrames := make([]MediaFrame, 0)

	// 1. 모든 extra data 먼저 추가 (비디오 → 오디오 순서)
	// 비디오 설정 프레임 (SPS/PPS) 먼저
	for codec := H264; codec <= AV1; codec++ {
		if extraFrame, exists := sb.extraData[codec]; exists {
			allFrames = append(allFrames, extraFrame)
			slog.Debug("Added video config frame", "codec", codec, "timestamp", extraFrame.Timestamp)
		}
	}
	// 오디오 설정 프레임
	for codec := AAC; codec <= MP3; codec++ {
		if extraFrame, exists := sb.extraData[codec]; exists {
			allFrames = append(allFrames, extraFrame)
			slog.Debug("Added audio config frame", "codec", codec, "timestamp", extraFrame.Timestamp)
		}
	}

	// 2. 키프레임부터 시작하는 프레임들만 포함 (디코딩 보장)
	if sb.lastKeyFrameIndex >= 0 && sb.lastKeyFrameIndex < len(sb.frames) {
		// 마지막 키프레임부터 끝까지의 프레임들
		keyFrameBasedFrames := sb.frames[sb.lastKeyFrameIndex:]
		allFrames = append(allFrames, keyFrameBasedFrames...)

		slog.Debug("Cached frames prepared with key frame GOP",
			"totalFrames", len(keyFrameBasedFrames),
			"keyFrameIndex", sb.lastKeyFrameIndex,
			"extraDataCount", len(sb.extraData))
	} else if len(sb.frames) > 0 {
		// 키프레임이 없는 경우 경고하고 모든 프레임 포함 (fallback)
		slog.Warn("No key frame available for new player, this may cause decoding issues",
			"totalFrames", len(sb.frames))
		allFrames = append(allFrames, sb.frames...)
	}

	return allFrames
}

// 캐시된 메타데이터를 반환합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) GetMetadata() map[string]string {
	if sb.metadata == nil {
		return nil
	}

	// Return a copy to avoid reference issues
	cached := make(map[string]string)
	for k, v := range sb.metadata {
		cached[k] = v
	}

	return cached
}

// Clear 모든 캐시된 데이터를 정리합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) Clear() {
	sb.extraData = make(map[Codec]MediaFrame)
	sb.frames = make([]MediaFrame, 0)
	sb.metadata = nil

	slog.Debug("Stream buffer cleared")
}

// 캐시된 데이터가 있으면 true를 반환합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) HasCachedData() bool {
	hasFrames := len(sb.frames) > 0
	hasExtraData := len(sb.extraData) > 0
	hasMetadata := sb.metadata != nil

	return hasFrames || hasExtraData || hasMetadata
}

// 캐시 통계를 반환합니다 (개선된 버전)
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (sb *StreamBuffer) GetCacheStats() map[string]any {
	bufferDuration := sb.getBufferDuration()
	keyFrameCount := sb.getKeyFrameCount()

	stats := map[string]any{
		// 기본 정보
		"total_frame_count": len(sb.frames),
		"extra_data_count":  len(sb.extraData),
		"has_metadata":      sb.metadata != nil,

		// 시간 기반 통계
		"buffer_duration_ms":     bufferDuration,
		"buffer_duration_sec":    float64(bufferDuration) / 1000.0,
		"min_buffer_duration_ms": sb.minBufferDurationMs,
		"max_buffer_duration_ms": sb.maxBufferDurationMs,

		// 키프레임 통계
		"key_frame_count":      keyFrameCount,
		"last_key_frame_index": sb.lastKeyFrameIndex,

		// 버퍼 효율성
		"buffer_utilization": sb.getBufferUtilization(),
		"max_frames_limit":   sb.maxFrames,
	}

	// 평균 키프레임 간격 계산 (키프레임이 2개 이상일 때)
	if avgInterval := sb.getAverageKeyFrameInterval(); avgInterval > 0 {
		stats["avg_keyframe_interval_ms"] = avgInterval
		stats["avg_keyframe_interval_sec"] = float64(avgInterval) / 1000.0
	}

	return stats
}

// getKeyFrameCount 버퍼 내 키프레임 개수 반환
func (sb *StreamBuffer) getKeyFrameCount() int {
	count := 0
	for _, frame := range sb.frames {
		if frame.IsKeyFrame() {
			count++
		}
	}
	return count
}

// getAverageKeyFrameInterval 평균 키프레임 간격 반환 (ms)
func (sb *StreamBuffer) getAverageKeyFrameInterval() uint32 {
	keyFrameTimes := make([]uint32, 0)

	for _, frame := range sb.frames {
		if frame.IsKeyFrame() {
			keyFrameTimes = append(keyFrameTimes, frame.Timestamp)
		}
	}

	if len(keyFrameTimes) < 2 {
		return 0
	}

	totalInterval := uint32(0)
	for i := 1; i < len(keyFrameTimes); i++ {
		totalInterval += keyFrameTimes[i] - keyFrameTimes[i-1]
	}

	return totalInterval / uint32(len(keyFrameTimes)-1)
}

// getBufferUtilization 버퍼 사용률 반환 (0.0 ~ 1.0)
func (sb *StreamBuffer) getBufferUtilization() float64 {
	if sb.maxBufferDurationMs == 0 {
		return 0.0
	}

	currentDuration := sb.getBufferDuration()
	if currentDuration >= sb.maxBufferDurationMs {
		return 1.0
	}

	return float64(currentDuration) / float64(sb.maxBufferDurationMs)
}
