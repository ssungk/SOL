package rtmp

import (
	"log/slog"
)

// Stream은 개별 스트림 정보를 관리
type Stream struct {
	name    string
	players map[*session]struct{} // player sessions 직접 참조

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

// addAudioSample은 오디오 샘플을 캐시에 추가 (최근 5개만 유지)
func (s *Stream) addAudioSample(timestamp uint32, data []byte) {
	// 오디오 프레임을 GOP 캐시에 추가
	audioFrame := CachedFrame{
		frameType: "audio",
		timestamp: timestamp,
		data:      data,
		msgType:   8, // audio
	}
	
	// 오디오 프레임을 GOP 캐시 끝에 추가
	s.gopCache = append(s.gopCache, audioFrame)
	
	// 오디오 샘플 수 제한 (최근 100개 프레임)
	if len(s.gopCache) > 100 {
		s.gopCache = s.gopCache[len(s.gopCache)-100:]
	}
}

// ProcessAudioData는 오디오 데이터를 받아서 캐시 업데이트 후 모든 플레이어에게 전송
func (s *Stream) ProcessAudioData(event AudioData) {
	// 오디오 샘플 캐시 (최근 5개 프레임만 유지)
	s.addAudioSample(event.Timestamp, event.Data)
	
	// 모든 플레이어에게 비동기 전송
	for player := range s.players {
		go s.sendAudioToPlayer(player, event)
	}
}

// ProcessVideoData는 비디오 데이터를 받아서 GOP 캐시 업데이트 후 모든 플레이어에게 전송
func (s *Stream) ProcessVideoData(event VideoData) {
	// GOP 캐시 업데이트
	s.AddVideoFrame(event.FrameType, event.Timestamp, event.Data)
	
	// 모든 플레이어에게 비동기 전송
	for player := range s.players {
		go s.sendVideoToPlayer(player, event)
	}
}

// ProcessMetaData는 메타데이터를 받아서 캐시 업데이트 후 모든 플레이어에게 전송
func (s *Stream) ProcessMetaData(event MetaData) {
	// 메타데이터 캐시
	s.SetMetadata(event.Metadata)
	
	// 모든 플레이어에게 비동기 전송
	for player := range s.players {
		go s.sendMetaDataToPlayer(player, event)
	}
}

// SetPublisher는 스트림의 발행자를 설정 (로깅만 수행)
func (s *Stream) SetPublisher(publisher *session) {
	slog.Info("Publisher set", "streamName", s.name, "sessionId", publisher.sessionId)
}

// RemovePublisher는 스트림의 발행자를 제거 (캐시 청소만 수행)
func (s *Stream) RemovePublisher() {
	// 캐시 청소
	s.gopCache = nil
	s.lastMetadata = nil
	slog.Info("Publisher removed and cache cleared", "streamName", s.name)
}

// AddPlayer는 플레이어를 추가하고 즉시 캐시된 데이터를 전송
func (s *Stream) AddPlayer(player *session) {
	s.players[player] = struct{}{}
	slog.Info("Player added", "streamName", s.name, "sessionId", player.sessionId, "playerCount", len(s.players))
	
	// 새로 입장한 플레이어에게 즉시 캐시된 데이터 전송
	go s.SendCachedDataToPlayer(player)
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
		// 새 GOP 시작 - 비디오 프레임만 청소하고 오디오는 유지
		newCache := make([]CachedFrame, 0)
		
		// 기존 오디오 프레임들 유지 (최근 10개만)
		audioCount := 0
		for i := len(s.gopCache) - 1; i >= 0 && audioCount < 10; i-- {
			if s.gopCache[i].msgType == 8 { // audio
				newCache = append([]CachedFrame{s.gopCache[i]}, newCache...)
				audioCount++
			}
		}
		
		// 새 키프레임 추가
		newCache = append(newCache, CachedFrame{
			frameType: frameType,
			timestamp: timestamp,
			data:      data,
			msgType:   9, // video
		})
		
		s.gopCache = newCache
		slog.Debug("New GOP started", "streamName", s.name, "timestamp", timestamp, "audioFramesKept", audioCount)
	} else if frameType == "inter frame" {
		// 키프레임 이후 프레임들 캐시에 추가
		if len(s.gopCache) > 0 { // 키프레임이 있는 경우만
			s.gopCache = append(s.gopCache, CachedFrame{
				frameType: frameType,
				timestamp: timestamp,
				data:      data,
				msgType:   9, // video
			})

			// 캐시 크기 제한 (최대 100프레임)
			if len(s.gopCache) > 100 {
				s.gopCache = s.gopCache[len(s.gopCache)-100:]
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



// GetName은 스트림 이름을 반환
func (s *Stream) GetName() string {
	return s.name
}

// IsActive는 스트림이 활성 상태인지 확인 (플레이어가 있는 경우 또는 캐시된 데이터가 있는 경우)
func (s *Stream) IsActive() bool {
	return len(s.players) > 0 || len(s.gopCache) > 0 || s.lastMetadata != nil
}

// CleanupSession은 세션 종료 시 스트림에서 해당 세션을 정리
func (s *Stream) CleanupSession(session *session) {
	// player 정리
	if _, exists := s.players[session]; exists {
		delete(s.players, session)
		slog.Info("Cleaned up player from stream", "streamName", s.name, "sessionId", session.sessionId, "playerCount", len(s.players))
	}

	// 발행자가 종료되면 캐시 청소 (이는 서버에서 PublishStopped 이벤트로 처리됨)
}

// sendAudioToPlayer는 플레이어에게 오디오 데이터를 전송
func (s *Stream) sendAudioToPlayer(player *session, event AudioData) {
	err := player.writer.writeAudioData(player.conn, event.Data, event.Timestamp)
	if err != nil {
		slog.Error("Failed to send audio to player", "streamName", s.name, "sessionId", player.sessionId, "err", err)
	}
}

// sendVideoToPlayer는 플레이어에게 비디오 데이터를 전송
func (s *Stream) sendVideoToPlayer(player *session, event VideoData) {
	err := player.writer.writeVideoData(player.conn, event.Data, event.Timestamp)
	if err != nil {
		slog.Error("Failed to send video to player", "streamName", s.name, "sessionId", player.sessionId, "err", err)
	}
}

// sendMetaDataToPlayer는 플레이어에게 메타데이터를 전송
func (s *Stream) sendMetaDataToPlayer(player *session, event MetaData) {
	err := player.writer.writeScriptData(player.conn, "onMetaData", event.Metadata)
	if err != nil {
		slog.Error("Failed to send metadata to player", "streamName", s.name, "sessionId", player.sessionId, "err", err)
	}
}

// SendCachedDataToPlayer는 새로 입장하는 플레이어에게 캐시된 데이터를 순서대로 전송
func (s *Stream) SendCachedDataToPlayer(player *session) {
	// 1. 메타데이터 먼저 전송 (동기)
	if s.lastMetadata != nil {
		s.sendMetaDataToPlayer(player, MetaData{
			SessionId:  "cache", // 캐시된 데이터는 cache로 표시
			StreamName: s.name,
			Metadata:   s.lastMetadata,
		})
		slog.Debug("Sent cached metadata to new player", "streamName", s.name, "sessionId", player.sessionId)
	}

	// 2. GOP 캐시가 있으면 순서대로 전송 (비동기로 전체 블록 전송)
	if len(s.gopCache) > 0 {
		go func() {
			slog.Debug("Sending GOP cache to new player", "streamName", s.name, "sessionId", player.sessionId, "frameCount", len(s.gopCache))
			
			// GOP 캐시를 타임스탬프 순서대로 전송
			for _, frame := range s.gopCache {
				if frame.msgType == 8 { // audio
					s.sendAudioToPlayer(player, AudioData{
						SessionId:  "cache",
						StreamName: s.name,
						Timestamp:  frame.timestamp,
						Data:       frame.data,
					})
				} else if frame.msgType == 9 { // video
					s.sendVideoToPlayer(player, VideoData{
						SessionId:  "cache",
						StreamName: s.name,
						Timestamp:  frame.timestamp,
						FrameType:  frame.frameType,
						Data:       frame.data,
					})
				}
			}
			
			slog.Debug("Finished sending GOP cache to new player", "streamName", s.name, "sessionId", player.sessionId)
		}()
	}
}
