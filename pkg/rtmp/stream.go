package rtmp

import (
	"log/slog"
	"sync"
)

// Stream은 개별 스트림 정보를 관리
type Stream struct {
	name        string
	publisherID string              // publisher session ID
	playerIDs   map[string]struct{} // player session IDs
	mu          sync.RWMutex

	// 메타데이터 캐시
	lastMetadata map[string]any

	// GOP 캐시 (키프레임 및 직후 프레임들)
	gopCache []CachedFrame
}

// CachedFrame은 캐시된 프레임 정보
type CachedFrame struct {
	frameType string
	timestamp uint32
	data      []byte
	msgType   uint8 // 8=audio, 9=video
}

// NewStream은 새로운 스트림을 생성
func NewStream(name string) *Stream {
	return &Stream{
		name:      name,
		playerIDs: make(map[string]struct{}),
		gopCache:  make([]CachedFrame, 0),
	}
}

// SetPublisher는 스트림의 발행자를 설정
func (s *Stream) SetPublisher(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publisherID = sessionID
	slog.Info("Publisher set", "streamName", s.name, "sessionId", sessionID)
}

// RemovePublisher는 스트림의 발행자를 제거
func (s *Stream) RemovePublisher() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.publisherID != "" {
		slog.Info("Publisher removed", "streamName", s.name, "sessionId", s.publisherID)
		s.publisherID = ""
		s.gopCache = nil // 캐시 청소
		s.lastMetadata = nil
	}
}

// GetPublisherID는 스트림의 발행자 ID를 반환
func (s *Stream) GetPublisherID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.publisherID
}

// AddPlayer는 플레이어를 추가
func (s *Stream) AddPlayer(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.playerIDs[sessionID] = struct{}{}
	slog.Info("Player added", "streamName", s.name, "sessionId", sessionID, "playerCount", len(s.playerIDs))
}

// RemovePlayer는 플레이어를 제거
func (s *Stream) RemovePlayer(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.playerIDs, sessionID)
	slog.Info("Player removed", "streamName", s.name, "sessionId", sessionID, "playerCount", len(s.playerIDs))
}

// GetPlayerIDs는 모든 플레이어 ID를 반환
func (s *Stream) GetPlayerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	playerIDs := make([]string, 0, len(s.playerIDs))
	for playerID := range s.playerIDs {
		playerIDs = append(playerIDs, playerID)
	}
	return playerIDs
}

// GetPlayerCount는 플레이어 수를 반환
func (s *Stream) GetPlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.playerIDs)
}

// SetMetadata는 메타데이터를 설정 및 캐시
func (s *Stream) SetMetadata(metadata map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastMetadata = metadata
	slog.Debug("Metadata cached", "streamName", s.name)
}

// GetMetadata는 캐시된 메타데이터를 반환
func (s *Stream) GetMetadata() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastMetadata
}

// AddVideoFrame은 비디오 프레임을 GOP 캐시에 추가
func (s *Stream) AddVideoFrame(frameType string, timestamp uint32, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if frameType == "key frame" {
		// 새 GOP 시작 - 기존 캐시 청소
		s.gopCache = []CachedFrame{
			{
				frameType: frameType,
				timestamp: timestamp,
				data:      data,
				msgType:   9, // video
			},
		}
		slog.Debug("New GOP started", "streamName", s.name, "timestamp", timestamp)
	} else if frameType == "inter frame" {
		// 키프레임 이후 프레임들 캐시에 추가
		if len(s.gopCache) > 0 { // 키프레임이 있는 경우만
			s.gopCache = append(s.gopCache, CachedFrame{
				frameType: frameType,
				timestamp: timestamp,
				data:      data,
				msgType:   9, // video
			})

			// 캐시 크기 제한 (GOP 당 최대 50프레임)
			if len(s.gopCache) > 50 {
				s.gopCache = s.gopCache[len(s.gopCache)-50:]
			}
		}
	}
}

// GetGOPCache는 GOP 캐시를 반환
func (s *Stream) GetGOPCache() []CachedFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 슬라이스 복사본 반환 (동시성 안전)
	cache := make([]CachedFrame, len(s.gopCache))
	copy(cache, s.gopCache)
	return cache
}

// GetName은 스트림 이름을 반환
func (s *Stream) GetName() string {
	return s.name
}

// IsActive는 스트림이 활성 상태인지 확인 (발행자 또는 플레이어가 있는 경우)
func (s *Stream) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.publisherID != "" || len(s.playerIDs) > 0
}
