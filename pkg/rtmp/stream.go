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

	// 비디오 캐시 (GOP 기반)
	videoCache VideoCache

	// 오디오 캐시 (최근 프레임들)
	audioCache AudioCache
}

// VideoFrame은 비디오 프레임 정보
type VideoFrame struct {
	frameType string // "key frame", "inter frame", "AVC sequence header", "AVC NALU"
	timestamp uint32
	data      [][]byte // Zero-copy payload chunks
}

// AudioFrame은 오디오 프레임 정보  
type AudioFrame struct {
	frameType string // "audio", "AAC sequence header"
	timestamp uint32
	data      [][]byte // Zero-copy payload chunks
}

// VideoCache는 비디오 프레임 캐시를 관리
type VideoCache struct {
	sequenceHeader *VideoFrame   // AVC sequence header
	gopFrames      []VideoFrame  // GOP 프레임들 (키프레임 + 후속 프레임들)
}

// AudioCache는 오디오 프레임 캐시를 관리
type AudioCache struct {
	sequenceHeader *AudioFrame   // AAC sequence header
	recentFrames   []AudioFrame  // 최근 오디오 프레임들
	maxFrames      int           // 최대 캐시할 오디오 프레임 수
}

// CachedFrame은 호환성을 위한 통합 프레임 정보 (기존 코드와의 호환성)
type CachedFrame struct {
	frameType string
	timestamp uint32
	data      []byte
	msgType   uint8 // 8=audio, 9=video
}

// copyChunks는 [][]byte를 deep copy하여 안전한 사본을 만든다
func copyChunks(chunks [][]byte) [][]byte {
	copied := make([][]byte, len(chunks))
	for i, chunk := range chunks {
		copied[i] = make([]byte, len(chunk))
		copy(copied[i], chunk)
	}
	return copied
}

// concatChunks는 [][]byte를 하나의 []byte로 합친다 (호환성 지원용)
func concatChunks(chunks [][]byte) []byte {
	totalLen := 0
	for _, chunk := range chunks {
		totalLen += len(chunk)
	}
	
	result := make([]byte, 0, totalLen)
	for _, chunk := range chunks {
		result = append(result, chunk...)
	}
	return result
}
// NewStream은 새로운 스트림을 생성
func NewStream(name string) *Stream {
	return &Stream{
		name:    name,
		players: make(map[*session]struct{}),
		videoCache: VideoCache{
			gopFrames: make([]VideoFrame, 0),
		},
		audioCache: AudioCache{
			recentFrames: make([]AudioFrame, 0),
			maxFrames:    10, // 최대 10개 오디오 프레임 캐시
		},
	}
}

// addAudioFrame은 오디오 프레임을 오디오 캐시에 추가
func (s *Stream) addAudioFrame(timestamp uint32, data [][]byte) {
	// AAC sequence header 특수 처리 - 첫 번째 청크를 기준으로 판단
	if len(data) > 0 && len(data[0]) > 1 && ((data[0][0]>>4)&0x0F) == 10 && data[0][1] == 0 {
		// AAC sequence header 설정
		s.audioCache.sequenceHeader = &AudioFrame{
			frameType: "AAC sequence header",
			timestamp: timestamp,
			data:      copyChunks(data), // Deep copy for safety
		}
		slog.Debug("AAC sequence header cached", "streamName", s.name, "timestamp", timestamp)
		return
	}

	// 일반 오디오 프레임 추가
	audioFrame := AudioFrame{
		frameType: "audio",
		timestamp: timestamp,
		data:      copyChunks(data), // Deep copy for safety
	}

	// 오디오 프레임을 최근 프레임 리스트에 추가
	s.audioCache.recentFrames = append(s.audioCache.recentFrames, audioFrame)

	// 최대 프레임 수 제한
	if len(s.audioCache.recentFrames) > s.audioCache.maxFrames {
		s.audioCache.recentFrames = s.audioCache.recentFrames[len(s.audioCache.recentFrames)-s.audioCache.maxFrames:]
	}
}

// ProcessAudioData는 오디오 데이터를 받아서 캐시 업데이트 후 모든 플레이어에게 전송
func (s *Stream) ProcessAudioData(event AudioData) {
	// 오디오 프레임 캐시
	s.addAudioFrame(event.Timestamp, event.Data)

	// 모든 플레이어에게 비동기 전송
	for player := range s.players {
		go s.sendAudioToPlayer(player, event)
	}
}

// ProcessVideoData는 비디오 데이터를 받아서 비디오 캐시 업데이트 후 모든 플레이어에게 전송
func (s *Stream) ProcessVideoData(event VideoData) {
	// 비디오 프레임 캐시 업데이트
	s.addVideoFrame(event.FrameType, event.Timestamp, event.Data)

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
	// 모든 캐시 청소
	s.videoCache = VideoCache{
		gopFrames: make([]VideoFrame, 0),
	}
	s.audioCache = AudioCache{
		recentFrames: make([]AudioFrame, 0),
		maxFrames:    10,
	}
	s.lastMetadata = nil
	slog.Info("Publisher removed and all caches cleared", "streamName", s.name)
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

// addVideoFrame은 비디오 프레임을 비디오 캐시에 추가
func (s *Stream) addVideoFrame(frameType string, timestamp uint32, data [][]byte) {
	// H.264 AVC sequence header는 별도 처리
	if frameType == "AVC sequence header" {
		// AVC sequence header 설정
		s.videoCache.sequenceHeader = &VideoFrame{
			frameType: frameType,
			timestamp: timestamp,
			data:      copyChunks(data), // Deep copy for safety
		}
		slog.Debug("AVC sequence header cached", "streamName", s.name, "timestamp", timestamp)
		return
	}

	if frameType == "key frame" || frameType == "AVC NALU" {
		// key frame인 경우 새 GOP 시작
		if frameType == "key frame" {
			// 새 GOP 시작 - 기존 GOP 프레임들 제거
			s.videoCache.gopFrames = make([]VideoFrame, 0)
			slog.Debug("New GOP started", "streamName", s.name, "timestamp", timestamp)
		}

		// 새 비디오 프레임 추가
		videoFrame := VideoFrame{
			frameType: frameType,
			timestamp: timestamp,
			data:      copyChunks(data), // Deep copy for safety
		}
		s.videoCache.gopFrames = append(s.videoCache.gopFrames, videoFrame)

	} else if frameType == "inter frame" {
		// 키프레임 이후 프레임들 캐시에 추가
		if len(s.videoCache.gopFrames) > 0 { // 키프레임이 있는 경우만
			videoFrame := VideoFrame{
				frameType: frameType,
				timestamp: timestamp,
				data:      copyChunks(data), // Deep copy for safety
			}
			s.videoCache.gopFrames = append(s.videoCache.gopFrames, videoFrame)

			// 캐시 크기 제한 (최대 50프레임)
			if len(s.videoCache.gopFrames) > 50 {
				s.videoCache.gopFrames = s.videoCache.gopFrames[len(s.videoCache.gopFrames)-50:]
			}
		}
	}
}

// GetGOPCache는 호환성을 위해 통합된 캐시를 CachedFrame 형태로 반환
func (s *Stream) GetGOPCache() []CachedFrame {
	cachedFrames := make([]CachedFrame, 0)

	// 1. AVC sequence header 추가
	if s.videoCache.sequenceHeader != nil {
		cachedFrames = append(cachedFrames, CachedFrame{
			frameType: s.videoCache.sequenceHeader.frameType,
			timestamp: s.videoCache.sequenceHeader.timestamp,
			data:      concatChunks(s.videoCache.sequenceHeader.data), // [][]byte를 []byte로 변환
			msgType:   9, // video
		})
	}

	// 2. AAC sequence header 추가
	if s.audioCache.sequenceHeader != nil {
		cachedFrames = append(cachedFrames, CachedFrame{
			frameType: "audio", // AAC sequence header를 일반 오디오로 표시
			timestamp: s.audioCache.sequenceHeader.timestamp,
			data:      concatChunks(s.audioCache.sequenceHeader.data), // [][]byte를 []byte로 변환
			msgType:   8, // audio
		})
	}

	// 3. 비디오 GOP 프레임들 추가
	for _, frame := range s.videoCache.gopFrames {
		cachedFrames = append(cachedFrames, CachedFrame{
			frameType: frame.frameType,
			timestamp: frame.timestamp,
			data:      concatChunks(frame.data), // [][]byte를 []byte로 변환
			msgType:   9, // video
		})
	}

	// 4. 최근 오디오 프레임들 추가
	for _, frame := range s.audioCache.recentFrames {
		cachedFrames = append(cachedFrames, CachedFrame{
			frameType: frame.frameType,
			timestamp: frame.timestamp,
			data:      concatChunks(frame.data), // [][]byte를 []byte로 변환
			msgType:   8, // audio
		})
	}

	return cachedFrames
}

// GetName은 스트림 이름을 반환
func (s *Stream) GetName() string {
	return s.name
}

// IsActive는 스트림이 활성 상태인지 확인 (플레이어가 있는 경우 또는 캐시된 데이터가 있는 경우)
func (s *Stream) IsActive() bool {
	return len(s.players) > 0 || 
		   len(s.videoCache.gopFrames) > 0 || 
		   len(s.audioCache.recentFrames) > 0 ||
		   s.videoCache.sequenceHeader != nil ||
		   s.audioCache.sequenceHeader != nil ||
		   s.lastMetadata != nil
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

	// 2. 캐시된 데이터가 있으면 순서대로 전솤 (비동기로 전체 블록 전송)
	hasCachedData := s.videoCache.sequenceHeader != nil || 
		           len(s.videoCache.gopFrames) > 0 || 
		           s.audioCache.sequenceHeader != nil ||
		           len(s.audioCache.recentFrames) > 0

	if hasCachedData {
		go func() {
			totalFrames := 0
			if s.videoCache.sequenceHeader != nil {
				totalFrames++
			}
			if s.audioCache.sequenceHeader != nil {
				totalFrames++
			}
			totalFrames += len(s.videoCache.gopFrames) + len(s.audioCache.recentFrames)

			slog.Debug("Sending cached data to new player", "streamName", s.name, "sessionId", player.sessionId, "frameCount", totalFrames)

			// 1) AVC sequence header 먼저 전송
			if s.videoCache.sequenceHeader != nil {
				s.sendVideoToPlayer(player, VideoData{
					SessionId:  "cache",
					StreamName: s.name,
					Timestamp:  s.videoCache.sequenceHeader.timestamp,
					FrameType:  s.videoCache.sequenceHeader.frameType,
					Data:       s.videoCache.sequenceHeader.data,
				})
			}

			// 2) AAC sequence header 전송
			if s.audioCache.sequenceHeader != nil {
				s.sendAudioToPlayer(player, AudioData{
					SessionId:  "cache",
					StreamName: s.name,
					Timestamp:  s.audioCache.sequenceHeader.timestamp,
					Data:       s.audioCache.sequenceHeader.data,
				})
			}

			// 3) 비디오 GOP 프레임들 전송
			for _, frame := range s.videoCache.gopFrames {
				s.sendVideoToPlayer(player, VideoData{
					SessionId:  "cache",
					StreamName: s.name,
					Timestamp:  frame.timestamp,
					FrameType:  frame.frameType,
					Data:       frame.data,
				})
			}

			// 4) 최근 오디오 프레임들 전송
			for _, frame := range s.audioCache.recentFrames {
				s.sendAudioToPlayer(player, AudioData{
					SessionId:  "cache",
					StreamName: s.name,
					Timestamp:  frame.timestamp,
					Data:       frame.data,
				})
			}

			slog.Debug("Finished sending cached data to new player", "streamName", s.name, "sessionId", player.sessionId)
		}()
	}
}
