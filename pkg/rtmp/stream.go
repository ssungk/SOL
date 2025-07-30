package rtmp

import (
	"log/slog"
	"sync"
)

// StreamManager는 모든 스트림을 관리하는 매니저
type StreamManager struct {
	streams map[string]*Stream // 스트림명 -> 스트림 정보
	mu      sync.RWMutex
}

// Stream은 개별 스트림 정보를 관리
type Stream struct {
	name      string
	publisher *session
	players   map[*session]struct{}
	mu        sync.RWMutex

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

// NewStreamManager는 새로운 스트림 매니저를 생성
func NewStreamManager() *StreamManager {
	return &StreamManager{
		streams: make(map[string]*Stream),
	}
}

// NewStream은 새로운 스트림을 생성
func NewStream(name string) *Stream {
	return &Stream{
		name:     name,
		players:  make(map[*session]struct{}),
		gopCache: make([]CachedFrame, 0),
	}
}

// GetOrCreateStream은 스트림을 가져오거나 생성
func (sm *StreamManager) GetOrCreateStream(streamName string) *Stream {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stream, exists := sm.streams[streamName]
	if !exists {
		stream = NewStream(streamName)
		sm.streams[streamName] = stream
		slog.Info("Created new stream", "streamName", streamName)
	}

	return stream
}

// GetStream은 스트림을 가져옴 (없으면 nil 반환)
func (sm *StreamManager) GetStream(streamName string) *Stream {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.streams[streamName]
}

// RemoveStream은 스트림을 제거
func (sm *StreamManager) RemoveStream(streamName string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.streams, streamName)
	slog.Info("Removed stream", "streamName", streamName)
}

// ListStreams는 모든 스트림 목록을 반환
func (sm *StreamManager) ListStreams() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	streams := make([]string, 0, len(sm.streams))
	for name := range sm.streams {
		streams = append(streams, name)
	}
	return streams
}

// GetStreamCount는 스트림 개수를 반환
func (sm *StreamManager) GetStreamCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.streams)
}

// SetPublisher는 스트림의 발행자를 설정
func (s *Stream) SetPublisher(publisher *session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publisher = publisher
	slog.Info("Publisher set", "streamName", s.name, "sessionId", publisher.sessionId)
}

// RemovePublisher는 스트림의 발행자를 제거
func (s *Stream) RemovePublisher() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.publisher != nil {
		slog.Info("Publisher removed", "streamName", s.name, "sessionId", s.publisher.sessionId)
		s.publisher = nil
		s.gopCache = nil // 캐시 청소
		s.lastMetadata = nil
	}
}

// GetPublisher는 스트림의 발행자를 반환
func (s *Stream) GetPublisher() *session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.publisher
}

// HasPublisher는 발행자가 있는지 확인
func (s *Stream) HasPublisher() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.publisher != nil
}

// AddPlayer는 플레이어를 추가
func (s *Stream) AddPlayer(player *session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.players[player] = struct{}{}
	slog.Info("Player added", "streamName", s.name, "sessionId", player.sessionId, "playerCount", len(s.players))
}

// RemovePlayer는 플레이어를 제거
func (s *Stream) RemovePlayer(player *session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.players, player)
	slog.Info("Player removed", "streamName", s.name, "sessionId", player.sessionId, "playerCount", len(s.players))
}

// GetPlayers는 모든 플레이어를 반환
func (s *Stream) GetPlayers() []*session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	players := make([]*session, 0, len(s.players))
	for player := range s.players {
		players = append(players, player)
	}
	return players
}

// GetPlayerCount는 플레이어 수를 반환
func (s *Stream) GetPlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.players)
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

// BroadcastToPlayers는 모든 플레이어에게 데이터를 브로드캐스트
func (s *Stream) BroadcastToPlayers(broadcastFunc func(*session)) {
	players := s.GetPlayers()
	for _, player := range players {
		go broadcastFunc(player)
	}
}

// GetName은 스트림 이름을 반환
func (s *Stream) GetName() string {
	return s.name
}

// GetInfo는 스트림 정보를 반환
func (s *Stream) GetInfo() (name string, hasPublisher bool, playerCount int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.name, s.publisher != nil, len(s.players)
}

// IsActive는 스트림이 활성 상태인지 확인 (발행자 또는 플레이어가 있는 경우)
func (s *Stream) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.publisher != nil || len(s.players) > 0
}

// ClearCache는 모든 캐시를 청소
func (s *Stream) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gopCache = nil
	s.lastMetadata = nil
	slog.Debug("Cache cleared", "streamName", s.name)
}
