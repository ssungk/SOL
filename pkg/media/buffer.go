package media

import (
	"log/slog"
)

type TrackBuffer struct {
	// 시간순으로 정렬된 모든 패킷 (Packet)
	packets []Packet

	// 코덱 설정 데이터 (SPS/PPS, AudioSpecificConfig 등)
	extraData map[Codec]Packet

	// 스트림 메타데이터 (width, height, framerate, audiocodecid 등)
	metadata map[string]string

	// 시간 기반 버퍼링 설정
	minBufferDurationMs uint64 // 최소 버퍼 시간 (ms) - 기본 2초
	maxBufferDurationMs uint64 // 최대 버퍼 시간 (ms) - 기본 10초
	maxPackets          int    // 안전장치로 최대 패킷 수

	// 키패킷 추적
	lastKeyPacketIndex int // 마지막 키패킷 위치
}

// 새로운 스트림 버퍼를 생성합니다 (기본 설정)
func NewTrackBuffer() *TrackBuffer {
	return NewTrackBufferWithConfig(2000, 10000, 1000) // 2-10초, 최대 1000패킷
}

// 설정 가능한 스트림 버퍼를 생성합니다
func NewTrackBufferWithConfig(minDurationMs, maxDurationMs uint64, maxPackets int) *TrackBuffer {
	return &TrackBuffer{
		packets:             make([]Packet, 0),
		extraData:           make(map[Codec]Packet),
		minBufferDurationMs: minDurationMs,
		maxBufferDurationMs: maxDurationMs,
		maxPackets:          maxPackets,
		lastKeyPacketIndex:  -1, // 아직 키패킷 없음
	}
}

// 패킷을 캐시에 추가합니다 (Packet 사용)
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) AddPacket(packet Packet) {
	// Handle extra data (config packets) separately
	if packet.Type == TypeConfig {
		tb.extraData[packet.Codec] = packet
		slog.Debug("Extra data cached", "codec", packet.Codec, "dts", packet.DTS)
		return
	}

	// 키패킷 추적 및 위치 기록
	if packet.IsKeyPacket() {
		tb.lastKeyPacketIndex = len(tb.packets)
		slog.Debug("New key packet detected", "dts", packet.DTS, "index", tb.lastKeyPacketIndex)
	}

	// Add packet to cache
	tb.packets = append(tb.packets, packet)

	// 시간 기준으로 오래된 패킷 정리
	tb.cleanupOldPackets()
}

// cleanupOldPackets 시간 기반으로 오래된 패킷을 정리합니다
func (tb *TrackBuffer) cleanupOldPackets() {
	if len(tb.packets) == 0 {
		return
	}

	currentTime := tb.getCurrentTimestamp() // 최신 패킷 타임스탬프
	minTime := currentTime - tb.minBufferDurationMs

	// 최소 버퍼 시간 이상 유지하면서 정리
	// 단, 키패킷은 보존하여 재생 연속성 확보
	cleanupIndex := 0
	for i, packet := range tb.packets {
		if packet.DTS >= minTime {
			break // 최소 시간 이후 패킷들은 모두 유지
		}

		// 키패킷이면서 최소 시간 내에 다른 키패킷이 있으면 제거 가능
		if packet.IsKeyPacket() {
			if tb.hasKeyPacketAfter(i, minTime) {
				cleanupIndex = i + 1
			}
		} else {
			cleanupIndex = i + 1
		}
	}

	// 정리 실행
	if cleanupIndex > 0 {
		tb.packets = tb.packets[cleanupIndex:]
		// 키패킷 인덱스 조정
		if tb.lastKeyPacketIndex >= cleanupIndex {
			tb.lastKeyPacketIndex -= cleanupIndex
		} else {
			tb.lastKeyPacketIndex = -1 // 키패킷이 정리됨
		}

		slog.Debug("Old packets cleaned up", "removedCount", cleanupIndex, "remainingCount", len(tb.packets))
	}

	// 안전장치: 최대 패킷 수 제한
	if len(tb.packets) > tb.maxPackets {
		excessCount := len(tb.packets) - tb.maxPackets
		tb.packets = tb.packets[excessCount:]
		// 키패킷 인덱스 조정
		if tb.lastKeyPacketIndex >= excessCount {
			tb.lastKeyPacketIndex -= excessCount
		} else {
			tb.lastKeyPacketIndex = -1
		}

		slog.Warn("Buffer overflow protection triggered", "removedCount", excessCount, "maxPackets", tb.maxPackets)
	}
}

// getCurrentTimestamp 현재(최신) 패킷의 DTS 반환
func (tb *TrackBuffer) getCurrentTimestamp() uint64 {
	if len(tb.packets) == 0 {
		return 0
	}
	return tb.packets[len(tb.packets)-1].DTS
}

// hasKeyPacketAfter 지정된 인덱스 이후에 최소 시간 내 키패킷이 있는지 확인
func (tb *TrackBuffer) hasKeyPacketAfter(index int, minTime uint64) bool {
	for i := index + 1; i < len(tb.packets); i++ {
		packet := tb.packets[i]
		if packet.IsKeyPacket() && packet.DTS >= minTime {
			return true
		}
	}
	return false
}

// getBufferDuration 현재 버퍼의 총 지속 시간 반환 (ms)
func (tb *TrackBuffer) getBufferDuration() uint64 {
	if len(tb.packets) < 2 {
		return 0
	}
	return tb.packets[len(tb.packets)-1].DTS - tb.packets[0].DTS
}

// 메타데이터를 캐시에 추가합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) AddMetadata(metadata map[string]string) {
	// Make a copy of metadata to avoid reference issues
	cached := make(map[string]string)
	for k, v := range metadata {
		cached[k] = v
	}

	tb.metadata = cached
	slog.Debug("Metadata cached")
}

// 새로운 플레이어를 위해 적절한 순서로 모든 캐시된 패킷을 반환합니다
// H.264 디코딩을 위해 키패킷부터 시작하는 GOP를 보장합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) GetCachedPackets() []Packet {
	allPackets := make([]Packet, 0)

	// 1. 모든 extra data 먼저 추가 (비디오 → 오디오 순서)
	// 비디오 설정 패킷 (SPS/PPS) 먼저
	for codec := H264; codec <= AV1; codec++ {
		if extraPacket, exists := tb.extraData[codec]; exists {
			allPackets = append(allPackets, extraPacket)
			slog.Debug("Added video config packet", "codec", codec, "timestamp", extraPacket.DTS)
		}
	}
	// 오디오 설정 패킷
	for codec := AAC; codec <= MP3; codec++ {
		if extraPacket, exists := tb.extraData[codec]; exists {
			allPackets = append(allPackets, extraPacket)
			slog.Debug("Added audio config packet", "codec", codec, "timestamp", extraPacket.DTS)
		}
	}

	// 2. 키패킷부터 시작하는 패킷들만 포함 (디코딩 보장)
	if tb.lastKeyPacketIndex >= 0 && tb.lastKeyPacketIndex < len(tb.packets) {
		// 마지막 키패킷부터 끝까지의 패킷들
		keyPacketBasedPackets := tb.packets[tb.lastKeyPacketIndex:]
		allPackets = append(allPackets, keyPacketBasedPackets...)

		slog.Debug("Cached packets prepared with key packet GOP",
			"totalPackets", len(keyPacketBasedPackets),
			"keyPacketIndex", tb.lastKeyPacketIndex,
			"extraDataCount", len(tb.extraData))
	} else if len(tb.packets) > 0 {
		// 키패킷이 없는 경우 경고하고 모든 패킷 포함 (fallback)
		slog.Warn("No key packet available for new player, this may cause decoding issues",
			"totalPackets", len(tb.packets))
		allPackets = append(allPackets, tb.packets...)
	}

	return allPackets
}

// 캐시된 메타데이터를 반환합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) GetMetadata() map[string]string {
	if tb.metadata == nil {
		return nil
	}

	// Return a copy to avoid reference issues
	cached := make(map[string]string)
	for k, v := range tb.metadata {
		cached[k] = v
	}

	return cached
}

// Clear 모든 캐시된 데이터를 정리합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) Clear() {
	tb.extraData = make(map[Codec]Packet)
	tb.packets = make([]Packet, 0)
	tb.metadata = nil

	slog.Debug("Stream buffer cleared")
}

// 캐시된 데이터가 있으면 true를 반환합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) HasCachedData() bool {
	hasPackets := len(tb.packets) > 0
	hasExtraData := len(tb.extraData) > 0
	hasMetadata := tb.metadata != nil

	return hasPackets || hasExtraData || hasMetadata
}

// 캐시 통계를 반환합니다 (개선된 버전)
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tb *TrackBuffer) GetCacheStats() map[string]any {
	bufferDuration := tb.getBufferDuration()
	keyPacketCount := tb.getKeyPacketCount()

	stats := map[string]any{
		// 기본 정보
		"total_packet_count": len(tb.packets),
		"extra_data_count":   len(tb.extraData),
		"has_metadata":       tb.metadata != nil,

		// 시간 기반 통계
		"buffer_duration_ms":     bufferDuration,
		"buffer_duration_sec":    float64(bufferDuration) / 1000.0,
		"min_buffer_duration_ms": tb.minBufferDurationMs,
		"max_buffer_duration_ms": tb.maxBufferDurationMs,

		// 키패킷 통계
		"key_packet_count":      keyPacketCount,
		"last_key_packet_index": tb.lastKeyPacketIndex,

		// 버퍼 효율성
		"buffer_utilization": tb.getBufferUtilization(),
		"max_packets_limit":  tb.maxPackets,
	}

	// 평균 키패킷 간격 계산 (키패킷이 2개 이상일 때)
	if avgInterval := tb.getAverageKeyPacketInterval(); avgInterval > 0 {
		stats["avg_keypacket_interval_ms"] = avgInterval
		stats["avg_keypacket_interval_sec"] = float64(avgInterval) / 1000.0
	}

	return stats
}

// getKeyPacketCount 버퍼 내 키패킷 개수 반환
func (tb *TrackBuffer) getKeyPacketCount() int {
	count := 0
	for _, packet := range tb.packets {
		if packet.IsKeyPacket() {
			count++
		}
	}
	return count
}

// getAverageKeyPacketInterval 평균 키패킷 간격 반환 (ms)
func (tb *TrackBuffer) getAverageKeyPacketInterval() uint64 {
	keyPacketTimes := make([]uint64, 0)

	for _, packet := range tb.packets {
		if packet.IsKeyPacket() {
			keyPacketTimes = append(keyPacketTimes, packet.DTS)
		}
	}

	if len(keyPacketTimes) < 2 {
		return 0
	}

	totalInterval := uint64(0)
	for i := 1; i < len(keyPacketTimes); i++ {
		totalInterval += keyPacketTimes[i] - keyPacketTimes[i-1]
	}

	return totalInterval / uint64(len(keyPacketTimes)-1)
}

// getBufferUtilization 버퍼 사용률 반환 (0.0 ~ 1.0)
func (tb *TrackBuffer) getBufferUtilization() float64 {
	if tb.maxBufferDurationMs == 0 {
		return 0.0
	}

	currentDuration := tb.getBufferDuration()
	if currentDuration >= tb.maxBufferDurationMs {
		return 1.0
	}

	return float64(currentDuration) / float64(tb.maxBufferDurationMs)
}
