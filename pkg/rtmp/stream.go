package rtmp

import (
	"log/slog"
)

// Stream은 개별 스트림 정보를 관리
type Stream struct {
	name      string
	publisher *session                 // publisher session 직접 참조
	players   map[*session]struct{}    // player sessions 직접 참조
	
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
		name:     name,
		players:  make(map[*session]struct{}),
		gopCache: make([]CachedFrame, 0),
	}
}

// SetPublisher는 스트림의 발행자를 설정
func (s *Stream) SetPublisher(publisher *session) {
	s.publisher = publisher
	slog.Info("Publisher set", "streamName", s.name, "sessionId", publisher.sessionId)
}

// RemovePublisher는 스트림의 발행자를 제거
func (s *Stream) RemovePublisher() {
	if s.publisher != nil {
		slog.Info("Publisher removed", "streamName", s.name, "sessionId", s.publisher.sessionId)
		s.publisher = nil
		// 캐시 청소
		s.gopCache = nil
		s.lastMetadata = nil
	}
}

// GetPublisher는 스트림의 발행자를 반환
func (s *Stream) GetPublisher() *session {
	return s.publisher
}

// HasPublisher는 발행자가 있는지 확인
func (s *Stream) HasPublisher() bool {
	return s.publisher != nil
}

// AddPlayer는 플레이어를 추가
func (s *Stream) AddPlayer(player *session) {
	s.players[player] = struct{}{}
	slog.Info("Player added", "streamName", s.name, "sessionId", player.sessionId, "playerCount", len(s.players))
}

// RemovePlayer는 플레이어를 제거
func (s *Stream) RemovePlayer(player *session) {
	delete(s.players, player)
	slog.Info("Player removed", "streamName", s.name, "sessionId", player.sessionId, "playerCount", len(s.players))
}

// GetPlayers는 모든 플레이어를 반환
func (s *Stream) GetPlayers() []*session {
	players := make([]*session, 0, len(s.players))
	for player := range s.players {
		players = append(players, player)
	}
	return players
}

// GetPlayerCount는 플레이어 수를 반환
func (s *Stream) GetPlayerCount() int {
	return len(s.players)
}

// SetMetadata는 메타데이터를 설정 및 캐시
func (s *Stream) SetMetadata(metadata map[string]any) {
	s.lastMetadata = metadata
	slog.Debug("Metadata cached", "streamName", s.name)
}

// GetMetadata는 캐시된 메타데이터를 반환
func (s *Stream) GetMetadata() map[string]any {
	return s.lastMetadata
}

// AddVideoFrame은 비디오 프레임을 GOP 캐시에 추가
func (s *Stream) AddVideoFrame(frameType string, timestamp uint32, data []byte) {
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

// IsActive는 스트림이 활성 상태인지 확인 (발행자 또는 플레이어가 있는 경우)
func (s *Stream) IsActive() bool {
	return s.publisher != nil || len(s.players) > 0
}

// CleanupSession은 세션 종료 시 스트림에서 해당 세션을 정리
func (s *Stream) CleanupSession(session *session) {
	// publisher 정리
	if s.publisher == session {
		slog.Info("Cleaning up publisher from stream", "streamName", s.name, "sessionId", session.sessionId)
		s.publisher = nil
		s.gopCache = nil
		s.lastMetadata = nil
	}

	// player 정리
	if _, exists := s.players[session]; exists {
		delete(s.players, session)
		slog.Info("Cleaned up player from stream", "streamName", s.name, "sessionId", session.sessionId, "playerCount", len(s.players))
	}
}
