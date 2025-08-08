package hls

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server HLS HTTP 서버
type Server struct {
	config       HLSConfig                    // 서버 설정
	httpServer   *http.Server                 // HTTP 서버
	playlists    map[string]*Playlist         // streamID -> Playlist
	segmentStore SegmentStore                 // 세그먼트 저장소
	packagers    map[string]*Packager         // streamID -> Packager
	mutex        sync.RWMutex                 // 동시성 제어
	ctx          context.Context              // 컨텍스트
	cancel       context.CancelFunc           // 취소 함수
	wg           sync.WaitGroup               // WaitGroup
	running      bool                         // 서버 실행 상태

	// MediaServer와의 통합을 위한 채널
	mediaServerChannel chan<- interface{}     // MediaServer로 이벤트 전송
	serverWg          *sync.WaitGroup         // MediaServer WaitGroup
}

// NewServer HLS 서버 생성
func NewServer(config HLSConfig, mediaServerChannel chan<- interface{}, serverWg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	server := &Server{
		config:             config,
		playlists:          make(map[string]*Playlist),
		segmentStore:       NewMemorySegmentStore(),
		packagers:          make(map[string]*Packager),
		ctx:                ctx,
		cancel:             cancel,
		mediaServerChannel: mediaServerChannel,
		serverWg:           serverWg,
	}
	
	// HTTP 서버 설정
	mux := http.NewServeMux()
	server.setupRoutes(mux)
	
	server.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: mux,
	}
	
	return server
}

// Start 서버 시작 (ProtocolServer 인터페이스 구현)
func (s *Server) Start() error {
	slog.Info("Starting HLS server", "port", s.config.Port)
	s.running = true
	
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HLS server error", "err", err)
		}
	}()
	
	// 정리 작업 고루틴 시작
	go s.cleanupLoop()
	
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("Stopping HLS server")
	s.running = false
	
	s.cancel()
	s.wg.Wait()
	
	// HTTP 서버 종료
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := s.httpServer.Shutdown(ctx); err != nil {
		slog.Error("Failed to shutdown HLS server gracefully", "err", err)
	}
	
	// 리소스 정리
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	for streamID := range s.playlists {
		s.cleanupStream(streamID)
	}
	
	slog.Info("HLS server stopped")
}

// Name 서버 이름 반환 (ProtocolServer 인터페이스 구현)
func (s *Server) Name() string {
	return "hls"
}



// setupRoutes HTTP 라우트 설정
func (s *Server) setupRoutes(mux *http.ServeMux) {
	// CORS 미들웨어 적용
	corsHandler := s.corsMiddleware
	
	// HLS 엔드포인트들
	mux.HandleFunc("/hls/", corsHandler(s.handleHLS))
	mux.HandleFunc("/api/hls/streams", corsHandler(s.handleStreamList))
	mux.HandleFunc("/api/hls/stats/", corsHandler(s.handleStats))
}

// handleHLS HLS 요청 처리
func (s *Server) handleHLS(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[5:] // "/hls/" 제거
	
	// URL 파싱: {streamID}/file.ext
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	
	streamID := parts[0]
	fileName := parts[1]
	
	slog.Debug("HLS request", "streamID", streamID, "fileName", fileName)
	
	// 파일 확장자에 따른 처리
	if strings.HasSuffix(fileName, ".m3u8") {
		s.handlePlaylist(w, r, streamID, fileName)
	} else if strings.HasSuffix(fileName, ".ts") || strings.HasSuffix(fileName, ".m4s") {
		s.handleSegment(w, r, streamID, fileName)
	} else if strings.HasSuffix(fileName, ".mp4") {
		s.handleInitSegment(w, r, streamID, fileName)
	} else {
		http.NotFound(w, r)
	}
}

// handlePlaylist 플레이리스트 요청 처리
func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request, streamID, fileName string) {
	s.mutex.RLock()
	playlist, exists := s.playlists[streamID]
	s.mutex.RUnlock()
	
	if !exists {
		http.NotFound(w, r)
		return
	}
	
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	
	// 마스터 플레이리스트 vs 미디어 플레이리스트
	if fileName == "playlist.m3u8" || fileName == "master.m3u8" {
		// 마스터 플레이리스트 (단일 품질용)
		variants := []PlaylistVariant{
			{
				Bandwidth: 1000000, // 1Mbps 추정
				URL:       "index.m3u8",
			},
		}
		data := playlist.GenerateMasterPlaylist(variants)
		w.Write(data)
	} else {
		// 미디어 플레이리스트
		data := playlist.GenerateMediaPlaylist()
		w.Write(data)
	}
}

// handleSegment 세그먼트 요청 처리
func (s *Server) handleSegment(w http.ResponseWriter, r *http.Request, streamID, fileName string) {
	// 파일명에서 세그먼트 인덱스 추출
	var segmentIndex int
	if strings.HasSuffix(fileName, ".ts") {
		fmt.Sscanf(fileName, "seg%d.ts", &segmentIndex)
		w.Header().Set("Content-Type", "video/mp2t")
	} else if strings.HasSuffix(fileName, ".m4s") {
		fmt.Sscanf(fileName, "seg%d.m4s", &segmentIndex)
		w.Header().Set("Content-Type", "video/mp4")
	}
	
	// 세그먼트 조회
	segment, err := s.segmentStore.GetSegment(streamID, segmentIndex)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	
	// 세그먼트 데이터 반환
	data, err := segment.GetData()
	if err != nil {
		http.Error(w, "Segment not ready", http.StatusServiceUnavailable)
		return
	}
	
	w.Header().Set("Cache-Control", "max-age=3600")
	w.Write(data)
}

// handleInitSegment 초기화 세그먼트 처리 (fMP4용)
func (s *Server) handleInitSegment(w http.ResponseWriter, r *http.Request, streamID, fileName string) {
	// fMP4 초기화 세그먼트 처리 (추후 구현)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "max-age=3600")
	
	// TODO: fMP4 초기화 세그먼트 데이터 반환
	http.NotFound(w, r)
}

// corsMiddleware CORS 미들웨어
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.config.CORSEnabled {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Range")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Content-Length")
			
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		next(w, r)
	}
}

// CreateStream 새 스트림 생성
func (s *Server) CreateStream(streamID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if _, exists := s.playlists[streamID]; exists {
		return fmt.Errorf("stream %s already exists", streamID)
	}
	
	// 플레이리스트 생성
	playlist := NewPlaylist(streamID, s.config)
	s.playlists[streamID] = playlist
	
	// 패키저 생성
	packager := NewPackager(streamID, s.config, s.segmentStore, playlist)
	s.packagers[streamID] = packager
	
	slog.Info("HLS stream created", "streamID", streamID)
	return nil
}

// RemoveStream 스트림 제거
func (s *Server) RemoveStream(streamID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.cleanupStream(streamID)
	slog.Info("HLS stream removed", "streamID", streamID)
}

// cleanupStream 스트림 정리 (mutex 필요)
func (s *Server) cleanupStream(streamID string) {
	if playlist, exists := s.playlists[streamID]; exists {
		playlist.Clear()
		delete(s.playlists, streamID)
	}
	
	if packager, exists := s.packagers[streamID]; exists {
		packager.Stop()
		delete(s.packagers, streamID)
	}
	
	s.segmentStore.Clear(streamID)
}

// GetPackager 패키저 반환
func (s *Server) GetPackager(streamID string) (*Packager, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	packager, exists := s.packagers[streamID]
	return packager, exists
}

// cleanupLoop 정리 작업 루프
func (s *Server) cleanupLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second) // 30초마다 정리
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			s.cleanupOldSegments()
		case <-s.ctx.Done():
			return
		}
	}
}

// cleanupOldSegments 오래된 세그먼트 정리
func (s *Server) cleanupOldSegments() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	for streamID, playlist := range s.playlists {
		playlist.RemoveOldSegments(s.config.PlaylistSize)
		s.segmentStore.RemoveOldSegments(streamID, s.config.PlaylistSize)
	}
}

// handleStreamList 스트림 목록 API 핸들러
func (s *Server) handleStreamList(w http.ResponseWriter, r *http.Request) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	streams := make([]StreamInfo, 0, len(s.playlists))
	
	for streamID, playlist := range s.playlists {
		info := StreamInfo{
			StreamID:     streamID,
			StartTime:    time.Now(), // TODO: 실제 시작 시간 추적
			LastUpdate:   time.Now(),
			SegmentCount: playlist.GetSegmentCount(),
			IsActive:     playlist.IsLive(),
		}
		streams = append(streams, info)
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"streams": streams,
		"total":   len(streams),
	})
}

// handleStats 통계 API 핸들러
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[15:] // "/api/hls/stats/" 제거
	streamID := path
	
	if streamID == "" {
		// 전체 통계
		s.mutex.RLock()
		stats := make(map[string]interface{})
		stats["total_streams"] = len(s.playlists)
		stats["active_streams"] = len(s.playlists) // TODO: 활성 스트림 카운트
		s.mutex.RUnlock()
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
		return
	}
	
	// 특정 스트림 통계
	s.mutex.RLock()
	playlist, exists := s.playlists[streamID]
	packager, packagerExists := s.packagers[streamID]
	s.mutex.RUnlock()
	
	if !exists {
		http.NotFound(w, r)
		return
	}
	
	stats := playlist.GetStats()
	if packagerExists {
		packagerStats := packager.GetStats()
		for k, v := range packagerStats {
			stats[k] = v
		}
	}
	
	storeStats, _ := s.segmentStore.GetStats(streamID)
	for k, v := range storeStats {
		stats["store_"+k] = v
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}