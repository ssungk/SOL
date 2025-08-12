package srt

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"sol/pkg/utils"
	"sync"
	"time"
)

// Server SRT 서버 구조체
type Server struct {
	config             SRTConfig
	mediaServerChannel chan<- any      // MediaServer로 이벤트 전송
	wg                 *sync.WaitGroup // 외부 WaitGroup 참조
	
	// 네트워크
	conn     *net.UDPConn
	ctx      context.Context
	cancel   context.CancelFunc
	
	// 세션 관리
	sessions        map[uint32]*Session // socketID -> Session
	streamSessions  map[string][]*Session // streamID -> Sessions (스트림별 세션 목록)
	sessionMutex    sync.RWMutex
	
	// 성능 최적화
	objectPool       *ObjectPool
	performanceMonitor *PerformanceMonitor
	workerPool       *WorkerPool
	connectionLimiter *ConnectionLimiter
	memoryChecker    *MemoryLimitChecker
	
	// 통계
	totalConnections uint64
	activeConnections uint64
	bytesReceived    uint64
	bytesSent        uint64
	
	// 성능 통계
	packetsProcessed uint64
	lastStatsTime    time.Time
}

// NewServer 새 SRT 서버 생성
func NewServer(config SRTConfig, mediaServerChannel chan<- any, serverWg *sync.WaitGroup) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	// 성능 최적화 컴포넌트 초기화
	objectPool := NewObjectPool()
	performanceMonitor := NewPerformanceMonitor()
	workerPool := NewWorkerPool(runtime.NumCPU() * 2) // CPU 수의 2배
	connectionLimiter := NewConnectionLimiter(10000)  // 최대 10,000 연결
	
	server := &Server{
		config:             config,
		mediaServerChannel: mediaServerChannel,
		wg:                 serverWg,
		ctx:                ctx,
		cancel:             cancel,
		sessions:           make(map[uint32]*Session),
		streamSessions:     make(map[string][]*Session),
		objectPool:         objectPool,
		performanceMonitor: performanceMonitor,
		workerPool:         workerPool,
		connectionLimiter:  connectionLimiter,
		lastStatsTime:      time.Now(),
	}
	
	// 메모리 사용량 모니터링 (500MB 제한)
	server.memoryChecker = NewMemoryLimitChecker(500, 10*time.Second, func() {
		slog.Warn("Memory usage exceeded limit, forcing GC")
		server.performanceMonitor.ForceGC()
	})
	
	return server
}

// Start 서버 시작 (ProtocolServer 인터페이스 구현)
func (s *Server) Start() error {
	// 설정 검증
	if err := s.config.ValidateConfig(); err != nil {
		return fmt.Errorf("invalid SRT config: %w", err)
	}
	
	// UDP 리스너 생성
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", s.config.Port))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}
	
	s.conn, err = net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", s.config.Port, err)
	}
	
	slog.Info("SRT server started", 
		"port", s.config.Port,
		"latency", s.config.Latency,
		"encryption", s.config.GetEncryptionString())
	
	// 패킷 수신 고루틴 시작
	go s.packetHandler()
	go s.eventLoop()
	
	return nil
}

// Stop 서버 중지 (ProtocolServer 인터페이스 구현)
func (s *Server) Stop() {
	slog.Info("SRT server stopping...")
	s.cancel()
	
	// 모든 세션 종료
	s.sessionMutex.Lock()
	for _, session := range s.sessions {
		session.Close()
	}
	s.sessionMutex.Unlock()
	
	// UDP 연결 종료
	if s.conn != nil {
		s.conn.Close()
	}
	
	// 성능 컴포넌트 정리
	if s.workerPool != nil {
		s.workerPool.Stop()
	}
	if s.memoryChecker != nil {
		s.memoryChecker.Stop()
	}
	
	slog.Info("SRT server stopped")
}

// Name 서버 이름 반환 (ProtocolServer 인터페이스 구현)
func (s *Server) Name() string {
	return "srt"
}

// eventLoop 이벤트 처리 루프
func (s *Server) eventLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer utils.CloseWithLog(s.conn)
	
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.ctx.Done():
			s.shutdown()
			return
		case <-ticker.C:
			s.maintenance()
		}
	}
}

// shutdown 서버 종료 처리
func (s *Server) shutdown() {
	slog.Info("SRT server shutdown completed")
}

// maintenance 주기적 유지보수 작업
func (s *Server) maintenance() {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()
	
	now := time.Now()
	for socketID, session := range s.sessions {
		// 비활성 세션 정리
		if now.Sub(session.LastActivity()) > s.config.Timeout {
			slog.Debug("Removing inactive SRT session", "socketID", socketID)
			go s.removeSession(socketID, "timeout")
		}
	}
}

// packetHandler UDP 패킷 처리
func (s *Server) packetHandler() {
	buffer := make([]byte, MTU)
	
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// 타임아웃 설정
			s.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			
			n, clientAddr, err := s.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // 타임아웃은 정상적인 상황
				}
				if s.ctx.Err() != nil {
					return // 서버 종료 중
				}
				slog.Error("Failed to read UDP packet", "err", err)
				continue
			}
			
			s.bytesReceived += uint64(n)
			
			// 패킷 처리
			go s.handlePacket(buffer[:n], clientAddr)
		}
	}
}

// handlePacket 패킷 처리
func (s *Server) handlePacket(data []byte, clientAddr *net.UDPAddr) {
	startTime := time.Now()
	
	if len(data) < UDTHeaderSize {
		slog.Debug("Received packet too small", "size", len(data))
		return
	}
	
	// 패킷 수 증가
	s.packetsProcessed++
	
	// 워커 풀을 통한 비동기 처리
	packetCopy := make([]byte, len(data))
	copy(packetCopy, data)
	
	s.workerPool.Submit(func() {
		s.processPacketAsync(packetCopy, clientAddr, startTime)
	})
}

// processPacketAsync 비동기 패킷 처리
func (s *Server) processPacketAsync(data []byte, clientAddr *net.UDPAddr, startTime time.Time) {
	defer func() {
		// 처리 시간 기록
		processingTime := time.Since(startTime)
		s.performanceMonitor.RecordProcessingTime(processingTime)
	}()
	
	// SRT 헤더 파싱
	socketID := s.parseSocketID(data)
	
	s.sessionMutex.RLock()
	session, exists := s.sessions[socketID]
	s.sessionMutex.RUnlock()
	
	if !exists {
		// 새 연결 처리
		if s.isHandshakePacket(data) {
			s.handleNewConnection(data, clientAddr)
		} else {
			slog.Debug("Received data for unknown session", "socketID", socketID)
		}
		return
	}
	
	// 기존 세션으로 패킷 전달
	session.HandlePacket(data)
}

// parseSocketID 패킷에서 소켓 ID 추출
func (s *Server) parseSocketID(data []byte) uint32 {
	packet, err := ParseUDTPacket(data)
	if err != nil {
		return 0
	}
	return packet.Header.DestSocketID
}

// isHandshakePacket 핸드셰이크 패킷 여부 확인
func (s *Server) isHandshakePacket(data []byte) bool {
	packet, err := ParseUDTPacket(data)
	if err != nil {
		return false
	}
	return IsHandshakePacket(packet)
}

// handleNewConnection 새 연결 처리
func (s *Server) handleNewConnection(data []byte, clientAddr *net.UDPAddr) {
	slog.Info("New SRT connection attempt", "client", clientAddr.String())
	
	// 연결 수 제한 확인
	if !s.connectionLimiter.Acquire() {
		slog.Warn("Connection limit reached", 
			"current", s.connectionLimiter.GetCurrent(), 
			"max", s.connectionLimiter.GetMax(),
			"client", clientAddr.String())
		s.sendConnectionRejection(clientAddr, 0, "Connection limit reached")
		return
	}
	
	// 핸드셰이크 요청 파싱
	handshakeReq, err := ParseSRTHandshake(data)
	if err != nil {
		slog.Error("Failed to parse SRT handshake", "err", err, "client", clientAddr.String())
		s.connectionLimiter.Release()
		return
	}
	
	// 클라이언트 소켓 ID 확인
	clientSocketID := handshakeReq.SocketID
	if clientSocketID == 0 {
		slog.Error("Invalid client socket ID", "client", clientAddr.String())
		s.connectionLimiter.Release()
		return
	}
	
	// 서버 소켓 ID 생성 (클라이언트 소켓 ID와 시간 기반)
	serverSocketID := uint32(time.Now().UnixNano()&0x7FFFFFFF) | 0x80000000 // MSB 설정으로 서버임을 표시
	
	// 이미 해당 클라이언트의 세션이 있는지 확인
	s.sessionMutex.RLock()
	for _, existingSession := range s.sessions {
		if existingSession.stats.PeerSocketID == clientSocketID {
			s.sessionMutex.RUnlock()
			slog.Debug("Session already exists for client", "clientSocketID", clientSocketID)
			// 기존 세션에 패킷 전달
			existingSession.HandlePacket(data)
			s.connectionLimiter.Release() // 새 연결이 아니므로 해제
			return
		}
	}
	s.sessionMutex.RUnlock()
	
	// 새 세션 생성
	session := NewSession(serverSocketID, clientAddr, s.config, s.mediaServerChannel, s)
	
	s.sessionMutex.Lock()
	s.sessions[serverSocketID] = session
	s.sessionMutex.Unlock()
	
	s.totalConnections++
	s.activeConnections++
	
	// 핸드셰이크 처리
	session.HandlePacket(data)
	
	slog.Info("SRT session created", 
		"serverSocketID", serverSocketID,
		"clientSocketID", clientSocketID,
		"client", clientAddr.String(),
		"totalConnections", s.totalConnections,
		"activeConnections", s.connectionLimiter.GetCurrent())
}

// sendConnectionRejection 연결 거절 전송
func (s *Server) sendConnectionRejection(clientAddr *net.UDPAddr, clientSocketID uint32, reason string) {
	slog.Info("Rejecting connection", "reason", reason, "client", clientAddr.String())
	
	// 거절 패킷 생성
	rejectionPacket := CreateControlPacket(ControlHandshake, clientSocketID, 0xFFFFFFFF)
	
	if err := s.SendToClient(clientAddr, rejectionPacket); err != nil {
		slog.Error("Failed to send connection rejection", "err", err)
	}
}

// removeSession 세션 제거
func (s *Server) removeSession(socketID uint32, reason string) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()
	
	if session, exists := s.sessions[socketID]; exists {
		session.Close()
		delete(s.sessions, socketID)
		s.activeConnections--
		
		// 연결 제한 해제
		s.connectionLimiter.Release()
		
		slog.Info("SRT session removed", 
			"socketID", socketID, 
			"reason", reason,
			"activeConnections", s.connectionLimiter.GetCurrent())
	}
}

// SendToClient 클라이언트에게 데이터 전송
func (s *Server) SendToClient(addr *net.UDPAddr, data []byte) error {
	if s.conn == nil {
		return fmt.Errorf("server not started")
	}
	
	n, err := s.conn.WriteToUDP(data, addr)
	if err != nil {
		return err
	}
	
	s.bytesSent += uint64(n)
	return nil
}

// GetStats 서버 통계 반환
func (s *Server) GetStats() map[string]any {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()
	
	// 성능 모니터링 업데이트
	s.performanceMonitor.UpdateMemoryStats()
	s.performanceMonitor.UpdateGoroutineStats()
	
	// 처리량 계산
	now := time.Now()
	timeDiff := now.Sub(s.lastStatsTime).Seconds()
	if timeDiff > 0 {
		packetsPerSecond := uint64(float64(s.packetsProcessed) / timeDiff)
		bytesPerSecond := uint64(float64(s.bytesReceived) / timeDiff)
		s.performanceMonitor.RecordThroughput(packetsPerSecond, bytesPerSecond)
	}
	s.lastStatsTime = now
	s.packetsProcessed = 0
	
	// 기본 통계
	stats := map[string]any{
		"total_connections":  s.totalConnections,
		"active_connections": s.connectionLimiter.GetCurrent(),
		"active_sessions":    len(s.sessions),
		"bytes_received":     s.bytesReceived,
		"bytes_sent":         s.bytesSent,
		"port":              s.config.Port,
		"latency":           s.config.Latency,
		"encryption":        s.config.GetEncryptionString(),
		"connection_limit":  s.connectionLimiter.GetMax(),
	}
	
	// 성능 통계 추가
	perfStats := s.performanceMonitor.GetStats()
	for k, v := range perfStats {
		stats[k] = v
	}
	
	return stats
}

// GetActiveSessionCount 활성 세션 수 반환
func (s *Server) GetActiveSessionCount() int {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()
	return len(s.sessions)
}

// GetSession 세션 반환
func (s *Server) GetSession(socketID uint32) (*Session, bool) {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()
	session, exists := s.sessions[socketID]
	return session, exists
}

// AddStreamSession 스트림에 세션 추가
func (s *Server) AddStreamSession(streamID string, session *Session) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()
	
	if s.streamSessions[streamID] == nil {
		s.streamSessions[streamID] = []*Session{}
	}
	
	// 중복 추가 방지
	for _, existingSession := range s.streamSessions[streamID] {
		if existingSession.socketID == session.socketID {
			return
		}
	}
	
	s.streamSessions[streamID] = append(s.streamSessions[streamID], session)
	slog.Debug("Session added to stream", "streamID", streamID, "socketID", session.socketID)
}

// RemoveStreamSession 스트림에서 세션 제거
func (s *Server) RemoveStreamSession(streamID string, session *Session) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()
	
	sessions := s.streamSessions[streamID]
	for i, existingSession := range sessions {
		if existingSession.socketID == session.socketID {
			// 슬라이스에서 제거
			s.streamSessions[streamID] = append(sessions[:i], sessions[i+1:]...)
			
			// 스트림에 세션이 없으면 맵에서 제거
			if len(s.streamSessions[streamID]) == 0 {
				delete(s.streamSessions, streamID)
			}
			
			slog.Debug("Session removed from stream", "streamID", streamID, "socketID", session.socketID)
			break
		}
	}
}

// GetStreamSessions 스트림의 모든 세션 반환
func (s *Server) GetStreamSessions(streamID string) []*Session {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()
	
	sessions := s.streamSessions[streamID]
	if sessions == nil {
		return []*Session{}
	}
	
	// 복사본 반환 (동시성 안전)
	result := make([]*Session, len(sessions))
	copy(result, sessions)
	return result
}

// GetStreamPublisher 스트림의 발행자 세션 반환
func (s *Server) GetStreamPublisher(streamID string) *Session {
	sessions := s.GetStreamSessions(streamID)
	for _, session := range sessions {
		if session.isPublisher {
			return session
		}
	}
	return nil
}

// GetStreamPlayers 스트림의 재생자 세션들 반환
func (s *Server) GetStreamPlayers(streamID string) []*Session {
	sessions := s.GetStreamSessions(streamID)
	var players []*Session
	for _, session := range sessions {
		if session.isPlayer {
			players = append(players, session)
		}
	}
	return players
}

// BroadcastToStreamPlayers 스트림의 모든 재생자에게 데이터 브로드캐스트
func (s *Server) BroadcastToStreamPlayers(streamID string, data []byte) {
	players := s.GetStreamPlayers(streamID)
	for _, player := range players {
		if err := player.sendDataPacket(data); err != nil {
			slog.Error("Failed to send data to player", 
				"streamID", streamID, 
				"playerSocketID", player.socketID, 
				"err", err)
		}
	}
}