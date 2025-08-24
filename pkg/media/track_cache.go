package media

import (
	"log/slog"
)

type TrackCache struct {
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

// 새로운 트랙 캐시를 생성합니다 (기본 설정)
func NewTrackCache() *TrackCache {
	return NewTrackCacheWithConfig(2000, 10000, 1000) // 2-10초, 최대 1000패킷
}

// 설정 가능한 트랙 캐시를 생성합니다
func NewTrackCacheWithConfig(minDurationMs, maxDurationMs uint64, maxPackets int) *TrackCache {
	return &TrackCache{
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
func (tc *TrackCache) AddPacket(packet Packet) {
	// Handle extra data (config packets) separately
	if packet.Type == TypeConfig {
		tc.extraData[packet.Codec] = packet
		slog.Debug("Extra data cached", "codec", packet.Codec, "dts", packet.DTS)
		return
	}

	// 키패킷 추적 및 위치 기록
	if packet.IsKeyPacket() {
		tc.lastKeyPacketIndex = len(tc.packets)
		slog.Debug("New key packet detected", "dts", packet.DTS, "index", tc.lastKeyPacketIndex)
	}

	// Add packet to cache
	tc.packets = append(tc.packets, packet)

	// 시간 기준으로 오래된 패킷 정리
	tc.cleanupOldPackets()
}

// cleanupOldPackets 시간 기반으로 오래된 패킷을 정리합니다
func (tc *TrackCache) cleanupOldPackets() {
	if len(tc.packets) == 0 {
		return
	}

	currentTime := tc.getCurrentTimestamp() // 최신 패킷 타임스탬프
	minTime := currentTime - tc.minBufferDurationMs

	// 최소 버퍼 시간 이상 유지하면서 정리
	// 단, 키패킷은 보존하여 재생 연속성 확보
	cleanupIndex := 0
	for i, packet := range tc.packets {
		if packet.DTS >= minTime {
			break // 최소 시간 이후 패킷들은 모두 유지
		}

		// 키패킷이면서 최소 시간 내에 다른 키패킷이 있으면 제거 가능
		if packet.IsKeyPacket() {
			if tc.hasKeyPacketAfter(i, minTime) {
				cleanupIndex = i + 1
			}
		} else {
			cleanupIndex = i + 1
		}
	}

	// 정리 실행
	if cleanupIndex > 0 {
		tc.packets = tc.packets[cleanupIndex:]
		// 키패킷 인덱스 조정
		if tc.lastKeyPacketIndex >= cleanupIndex {
			tc.lastKeyPacketIndex -= cleanupIndex
		} else {
			tc.lastKeyPacketIndex = -1 // 키패킷이 정리됨
		}

		}

	// 안전장치: 최대 패킷 수 제한
	if len(tc.packets) > tc.maxPackets {
		excessCount := len(tc.packets) - tc.maxPackets
		tc.packets = tc.packets[excessCount:]
		// 키패킷 인덱스 조정
		if tc.lastKeyPacketIndex >= excessCount {
			tc.lastKeyPacketIndex -= excessCount
		} else {
			tc.lastKeyPacketIndex = -1
		}

		slog.Warn("Buffer overflow protection triggered", "removedCount", excessCount, "maxPackets", tc.maxPackets)
	}
}

// getCurrentTimestamp 현재(최신) 패킷의 DTS 반환
func (tc *TrackCache) getCurrentTimestamp() uint64 {
	if len(tc.packets) == 0 {
		return 0
	}
	return tc.packets[len(tc.packets)-1].DTS
}

// hasKeyPacketAfter 지정된 인덱스 이후에 최소 시간 내 키패킷이 있는지 확인
func (tc *TrackCache) hasKeyPacketAfter(index int, minTime uint64) bool {
	for i := index + 1; i < len(tc.packets); i++ {
		packet := tc.packets[i]
		if packet.IsKeyPacket() && packet.DTS >= minTime {
			return true
		}
	}
	return false
}

// getBufferDuration 현재 버퍼의 총 지속 시간 반환 (ms)
func (tc *TrackCache) getBufferDuration() uint64 {
	if len(tc.packets) < 2 {
		return 0
	}
	return tc.packets[len(tc.packets)-1].DTS - tc.packets[0].DTS
}

// 메타데이터를 캐시에 추가합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tc *TrackCache) AddMetadata(metadata map[string]string) {
	// Make a copy of metadata to avoid reference issues
	cached := make(map[string]string)
	for k, v := range metadata {
		cached[k] = v
	}

	tc.metadata = cached
	slog.Debug("Metadata cached")
}

// 새로운 플레이어를 위해 적절한 순서로 모든 캐시된 패킷을 반환합니다
// H.264 디코딩을 위해 키패킷부터 시작하는 GOP를 보장합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tc *TrackCache) GetCachedPackets() []Packet {
	allPackets := make([]Packet, 0)

	// 1. 모든 extra data 먼저 추가 (비디오 → 오디오 순서)
	// 비디오 설정 패킷 (SPS/PPS) 먼저
	for codec := H264; codec <= AV1; codec++ {
		if extraPacket, exists := tc.extraData[codec]; exists {
			allPackets = append(allPackets, extraPacket)
			slog.Debug("Added video config packet", "codec", codec, "timestamp", extraPacket.DTS)
		}
	}
	// 오디오 설정 패킷
	for codec := AAC; codec <= MP3; codec++ {
		if extraPacket, exists := tc.extraData[codec]; exists {
			allPackets = append(allPackets, extraPacket)
			slog.Debug("Added audio config packet", "codec", codec, "timestamp", extraPacket.DTS)
		}
	}

	// 2. 키패킷부터 시작하는 패킷들만 포함 (디코딩 보장)
	if tc.lastKeyPacketIndex >= 0 && tc.lastKeyPacketIndex < len(tc.packets) {
		// 마지막 키패킷부터 끝까지의 패킷들
		keyPacketBasedPackets := tc.packets[tc.lastKeyPacketIndex:]
		allPackets = append(allPackets, keyPacketBasedPackets...)

		slog.Debug("Cached packets prepared with key packet GOP",
			"totalPackets", len(keyPacketBasedPackets),
			"keyPacketIndex", tc.lastKeyPacketIndex,
			"extraDataCount", len(tc.extraData))
	} else if len(tc.packets) > 0 {
		// 키패킷이 없는 경우 경고하고 모든 패킷 포함 (fallback)
		slog.Warn("No key packet available for new player, this may cause decoding issues",
			"totalPackets", len(tc.packets))
		allPackets = append(allPackets, tc.packets...)
	}

	return allPackets
}

// 캐시된 메타데이터를 반환합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tc *TrackCache) GetMetadata() map[string]string {
	if tc.metadata == nil {
		return nil
	}

	// Return a copy to avoid reference issues
	cached := make(map[string]string)
	for k, v := range tc.metadata {
		cached[k] = v
	}

	return cached
}

// Clear 모든 캐시된 데이터를 정리합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tc *TrackCache) Clear() {
	tc.extraData = make(map[Codec]Packet)
	tc.packets = make([]Packet, 0)
	tc.metadata = nil

	slog.Debug("Stream buffer cleared")
}

// 캐시된 데이터가 있으면 true를 반환합니다
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tc *TrackCache) HasCachedData() bool {
	hasPackets := len(tc.packets) > 0
	hasExtraData := len(tc.extraData) > 0
	hasMetadata := tc.metadata != nil

	return hasPackets || hasExtraData || hasMetadata
}

// 캐시 통계를 반환합니다 (개선된 버전)
// 이벤트 드리븐: 이벤트 루프에서 호출되어야 합니다
func (tc *TrackCache) GetCacheStats() map[string]any {
	bufferDuration := tc.getBufferDuration()
	keyPacketCount := tc.getKeyPacketCount()

	stats := map[string]any{
		// 기본 정보
		"total_packet_count": len(tc.packets),
		"extra_data_count":   len(tc.extraData),
		"has_metadata":       tc.metadata != nil,

		// 시간 기반 통계
		"buffer_duration_ms":     bufferDuration,
		"buffer_duration_sec":    float64(bufferDuration) / 1000.0,
		"min_buffer_duration_ms": tc.minBufferDurationMs,
		"max_buffer_duration_ms": tc.maxBufferDurationMs,

		// 키패킷 통계
		"key_packet_count":      keyPacketCount,
		"last_key_packet_index": tc.lastKeyPacketIndex,

		// 버퍼 효율성
		"buffer_utilization": tc.getBufferUtilization(),
		"max_packets_limit":  tc.maxPackets,
	}

	// 평균 키패킷 간격 계산 (키패킷이 2개 이상일 때)
	if avgInterval := tc.getAverageKeyPacketInterval(); avgInterval > 0 {
		stats["avg_keypacket_interval_ms"] = avgInterval
		stats["avg_keypacket_interval_sec"] = float64(avgInterval) / 1000.0
	}

	return stats
}

// getKeyPacketCount 버퍼 내 키패킷 개수 반환
func (tc *TrackCache) getKeyPacketCount() int {
	count := 0
	for _, packet := range tc.packets {
		if packet.IsKeyPacket() {
			count++
		}
	}
	return count
}

// getAverageKeyPacketInterval 평균 키패킷 간격 반환 (ms)
func (tc *TrackCache) getAverageKeyPacketInterval() uint64 {
	keyPacketTimes := make([]uint64, 0)

	for _, packet := range tc.packets {
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
func (tc *TrackCache) getBufferUtilization() float64 {
	if tc.maxBufferDurationMs == 0 {
		return 0.0
	}

	currentDuration := tc.getBufferDuration()
	if currentDuration >= tc.maxBufferDurationMs {
		return 1.0
	}

	return float64(currentDuration) / float64(tc.maxBufferDurationMs)
}
