package rtmp2

import (
	"log/slog"
	"sol/pkg/media"
	"sync"
)

// SimpleStreamManager pkg/media Stream을 관리하는 간단한 구현체
type SimpleStreamManager struct {
	streams map[string]*media.Stream
	mutex   sync.RWMutex
}

// NewSimpleStreamManager 새로운 간단한 스트림 매니저 생성
func NewSimpleStreamManager() *SimpleStreamManager {
	return &SimpleStreamManager{
		streams: make(map[string]*media.Stream),
	}
}

// GetOrCreateStream 스트림을 가져오거나 생성
func (sm *SimpleStreamManager) GetOrCreateStream(streamId string) *media.Stream {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	stream, exists := sm.streams[streamId]
	if !exists {
		stream = media.NewStream(streamId)
		sm.streams[streamId] = stream
		slog.Info("Created new media stream", "streamId", streamId)
	}
	
	return stream
}

// GetStream 스트림 가져오기 (없으면 nil 반환)
func (sm *SimpleStreamManager) GetStream(streamId string) *media.Stream {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	return sm.streams[streamId]
}

// RemoveStream 스트림 제거
func (sm *SimpleStreamManager) RemoveStream(streamId string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	if stream, exists := sm.streams[streamId]; exists {
		stream.Stop()
		delete(sm.streams, streamId)
		slog.Info("Removed media stream", "streamId", streamId)
	}
}

// GetAllStreams 모든 스트림 반환 (복사본)
func (sm *SimpleStreamManager) GetAllStreams() map[string]*media.Stream {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	streams := make(map[string]*media.Stream)
	for id, stream := range sm.streams {
		streams[id] = stream
	}
	
	return streams
}

// GetStreamCount 스트림 개수 반환
func (sm *SimpleStreamManager) GetStreamCount() int {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	return len(sm.streams)
}

// Cleanup 모든 스트림 정리
func (sm *SimpleStreamManager) Cleanup() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	for streamId, stream := range sm.streams {
		stream.Stop()
		slog.Info("Cleaned up stream", "streamId", streamId)
	}
	
	sm.streams = make(map[string]*media.Stream)
	slog.Info("All streams cleaned up")
}