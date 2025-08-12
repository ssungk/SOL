package srt

import (
	"container/list"
	"log/slog"
	"sync"
	"time"
)

// PacketState 패킷 상태
type PacketState int

const (
	PacketStateSent      PacketState = 1
	PacketStateAcked     PacketState = 2
	PacketStateNaked     PacketState = 3
	PacketStateRetransmitted PacketState = 4
	PacketStateLost      PacketState = 5
)

// SentPacket 전송된 패킷 정보
type SentPacket struct {
	SeqNum       uint32
	Data         []byte
	SentTime     time.Time
	RetryCount   int
	State        PacketState
	LastRetry    time.Time
}

// ReceivedPacket 수신된 패킷 정보  
type ReceivedPacket struct {
	SeqNum       uint32
	Data         []byte
	ReceivedTime time.Time
	Processed    bool
}

// SRTReliability SRT 신뢰성 관리 구조체
type SRTReliability struct {
	// 전송 관리
	sendBuffer     map[uint32]*SentPacket // 전송된 패킷들
	sendBufferMutex sync.RWMutex
	nextSeqNum     uint32
	
	// 수신 관리
	receiveBuffer  map[uint32]*ReceivedPacket // 수신된 패킷들
	receiveBufferMutex sync.RWMutex
	expectedSeqNum uint32
	
	// 손실 감지
	lossDetection  *LossDetector
	
	// ACK/NAK 관리
	ackQueue       *list.List // ACK 대기열
	nakQueue       *list.List // NAK 대기열
	queueMutex     sync.Mutex
	
	// 재전송 관리
	retransmitQueue *list.List
	retransmitMutex sync.Mutex
	
	// 통계
	stats ReliabilityStats
	statsMutex sync.RWMutex
	
	// 설정
	config ReliabilityConfig
	
	// 세션 참조
	session *Session
}

// ReliabilityConfig 신뢰성 설정
type ReliabilityConfig struct {
	MaxRetries        int           // 최대 재전송 횟수
	RetryInterval     time.Duration // 재전송 간격
	AckTimeout        time.Duration // ACK 타임아웃
	LossDetectionTime time.Duration // 손실 감지 시간
	BufferSize        int           // 버퍼 크기
}

// ReliabilityStats 신뢰성 통계
type ReliabilityStats struct {
	PacketsSent        uint64
	PacketsReceived    uint64
	PacketsLost        uint64
	PacketsRetransmitted uint64
	AcksSent           uint64
	AcksReceived       uint64
	NaksSent           uint64
	NaksReceived       uint64
	DuplicatePackets   uint64
	OutOfOrderPackets  uint64
}

// LossDetector 패킷 손실 감지기
type LossDetector struct {
	missingPackets map[uint32]time.Time // 누락된 패킷과 감지 시간
	mutex          sync.RWMutex
	detectionTime  time.Duration
}

// NewSRTReliability 새 신뢰성 관리자 생성
func NewSRTReliability(session *Session, config ReliabilityConfig) *SRTReliability {
	reliability := &SRTReliability{
		sendBuffer:      make(map[uint32]*SentPacket),
		receiveBuffer:   make(map[uint32]*ReceivedPacket),
		ackQueue:        list.New(),
		nakQueue:        list.New(), 
		retransmitQueue: list.New(),
		config:          config,
		session:         session,
		lossDetection: &LossDetector{
			missingPackets: make(map[uint32]time.Time),
			detectionTime:  config.LossDetectionTime,
		},
		nextSeqNum:     1,
		expectedSeqNum: 1,
	}
	
	// 기본 설정값 적용
	if config.MaxRetries == 0 {
		reliability.config.MaxRetries = MaxRetries
	}
	if config.RetryInterval == 0 {
		reliability.config.RetryInterval = time.Duration(RetryInterval) * time.Millisecond
	}
	if config.AckTimeout == 0 {
		reliability.config.AckTimeout = 1000 * time.Millisecond
	}
	if config.LossDetectionTime == 0 {
		reliability.config.LossDetectionTime = 500 * time.Millisecond
	}
	if config.BufferSize == 0 {
		reliability.config.BufferSize = 1000
	}
	
	return reliability
}

// SendPacket 패킷 전송 (신뢰성 관리 추가)
func (r *SRTReliability) SendPacket(data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, nil
	}
	
	r.sendBufferMutex.Lock()
	seqNum := r.nextSeqNum
	r.nextSeqNum++
	r.sendBufferMutex.Unlock()
	
	// 전송된 패킷 정보 저장
	sentPacket := &SentPacket{
		SeqNum:     seqNum,
		Data:       make([]byte, len(data)),
		SentTime:   time.Now(),
		RetryCount: 0,
		State:      PacketStateSent,
	}
	copy(sentPacket.Data, data)
	
	r.sendBufferMutex.Lock()
	r.sendBuffer[seqNum] = sentPacket
	// 버퍼 크기 제한
	if len(r.sendBuffer) > r.config.BufferSize {
		r.cleanupOldPackets()
	}
	r.sendBufferMutex.Unlock()
	
	// 실제 패킷 전송
	err := r.session.sendDataPacket(data)
	if err != nil {
		r.sendBufferMutex.Lock()
		delete(r.sendBuffer, seqNum)
		r.sendBufferMutex.Unlock()
		return 0, err
	}
	
	r.statsMutex.Lock()
	r.stats.PacketsSent++
	r.statsMutex.Unlock()
	
	slog.Debug("Packet sent with reliability tracking", 
		"socketID", r.session.socketID, 
		"seqNum", seqNum, 
		"dataSize", len(data))
	
	return seqNum, nil
}

// ReceivePacket 패킷 수신 처리 (순서 보장)
func (r *SRTReliability) ReceivePacket(seqNum uint32, data []byte) error {
	now := time.Now()
	
	r.receiveBufferMutex.Lock()
	defer r.receiveBufferMutex.Unlock()
	
	// 중복 패킷 확인
	if existingPacket, exists := r.receiveBuffer[seqNum]; exists {
		if existingPacket.Processed {
			r.statsMutex.Lock()
			r.stats.DuplicatePackets++
			r.statsMutex.Unlock()
			slog.Debug("Duplicate packet received", 
				"socketID", r.session.socketID, 
				"seqNum", seqNum)
			return nil
		}
	}
	
	// 수신된 패킷 저장
	receivedPacket := &ReceivedPacket{
		SeqNum:       seqNum,
		Data:         make([]byte, len(data)),
		ReceivedTime: now,
		Processed:    false,
	}
	copy(receivedPacket.Data, data)
	r.receiveBuffer[seqNum] = receivedPacket
	
	r.statsMutex.Lock()
	r.stats.PacketsReceived++
	r.statsMutex.Unlock()
	
	// 순서 확인
	if seqNum < r.expectedSeqNum {
		// 늦게 도착한 패킷 (Out-of-order)
		r.statsMutex.Lock()
		r.stats.OutOfOrderPackets++
		r.statsMutex.Unlock()
		slog.Debug("Out-of-order packet received", 
			"socketID", r.session.socketID, 
			"seqNum", seqNum, 
			"expected", r.expectedSeqNum)
	} else if seqNum > r.expectedSeqNum {
		// 누락된 패킷들 감지
		r.detectLostPackets(r.expectedSeqNum, seqNum)
	}
	
	// 연속된 패킷들 처리
	r.processConsecutivePackets()
	
	// ACK 전송 스케줄링
	r.scheduleAck(seqNum)
	
	slog.Debug("Packet received with reliability tracking", 
		"socketID", r.session.socketID, 
		"seqNum", seqNum, 
		"expected", r.expectedSeqNum, 
		"dataSize", len(data))
	
	return nil
}

// ProcessAck ACK 패킷 처리
func (r *SRTReliability) ProcessAck(ackSeqNum uint32) {
	r.sendBufferMutex.Lock()
	defer r.sendBufferMutex.Unlock()
	
	if sentPacket, exists := r.sendBuffer[ackSeqNum]; exists {
		sentPacket.State = PacketStateAcked
		delete(r.sendBuffer, ackSeqNum)
		
		r.statsMutex.Lock()
		r.stats.AcksReceived++
		r.statsMutex.Unlock()
		
		slog.Debug("ACK received for packet", 
			"socketID", r.session.socketID, 
			"ackSeqNum", ackSeqNum,
			"rtt", time.Since(sentPacket.SentTime))
	}
}

// ProcessNak NAK 패킷 처리 (재전송 요청)
func (r *SRTReliability) ProcessNak(nakSeqNum uint32) {
	r.sendBufferMutex.RLock()
	sentPacket, exists := r.sendBuffer[nakSeqNum]
	r.sendBufferMutex.RUnlock()
	
	if !exists {
		slog.Debug("NAK received for unknown packet", 
			"socketID", r.session.socketID, 
			"nakSeqNum", nakSeqNum)
		return
	}
	
	// 재전송 제한 확인
	if sentPacket.RetryCount >= r.config.MaxRetries {
		slog.Warn("Max retries reached for packet", 
			"socketID", r.session.socketID, 
			"nakSeqNum", nakSeqNum, 
			"retryCount", sentPacket.RetryCount)
		
		r.sendBufferMutex.Lock()
		sentPacket.State = PacketStateLost
		r.sendBufferMutex.Unlock()
		
		r.statsMutex.Lock()
		r.stats.PacketsLost++
		r.statsMutex.Unlock()
		return
	}
	
	// 재전송 큐에 추가
	r.retransmitMutex.Lock()
	r.retransmitQueue.PushBack(nakSeqNum)
	r.retransmitMutex.Unlock()
	
	r.statsMutex.Lock()
	r.stats.NaksReceived++
	r.statsMutex.Unlock()
	
	slog.Debug("NAK received, packet queued for retransmission", 
		"socketID", r.session.socketID, 
		"nakSeqNum", nakSeqNum)
}

// detectLostPackets 누락된 패킷들 감지
func (r *SRTReliability) detectLostPackets(from, to uint32) {
	for seqNum := from; seqNum < to; seqNum++ {
		if _, exists := r.receiveBuffer[seqNum]; !exists {
			r.lossDetection.mutex.Lock()
			if _, detected := r.lossDetection.missingPackets[seqNum]; !detected {
				r.lossDetection.missingPackets[seqNum] = time.Now()
				slog.Debug("Missing packet detected", 
					"socketID", r.session.socketID, 
					"seqNum", seqNum)
			}
			r.lossDetection.mutex.Unlock()
		}
	}
}

// processConsecutivePackets 연속된 패킷들 처리
func (r *SRTReliability) processConsecutivePackets() {
	for {
		packet, exists := r.receiveBuffer[r.expectedSeqNum]
		if !exists || packet.Processed {
			break
		}
		
		// 패킷 처리
		packet.Processed = true
		r.expectedSeqNum++
		
		// 실제 데이터를 상위 계층으로 전달
		select {
		case r.session.dataChannel <- packet.Data:
		default:
			slog.Debug("Data channel full, dropping processed packet", 
				"socketID", r.session.socketID, 
				"seqNum", packet.SeqNum)
		}
		
		slog.Debug("Packet processed in order", 
			"socketID", r.session.socketID, 
			"seqNum", packet.SeqNum)
	}
}

// scheduleAck ACK 전송 스케줄링
func (r *SRTReliability) scheduleAck(seqNum uint32) {
	r.queueMutex.Lock()
	r.ackQueue.PushBack(seqNum)
	r.queueMutex.Unlock()
}

// scheduleNak NAK 전송 스케줄링  
func (r *SRTReliability) scheduleNak(seqNum uint32) {
	r.queueMutex.Lock()
	r.nakQueue.PushBack(seqNum)
	r.queueMutex.Unlock()
}

// ProcessQueues ACK/NAK/재전송 큐 처리
func (r *SRTReliability) ProcessQueues() {
	r.processAckQueue()
	r.processNakQueue()
	r.processRetransmitQueue()
	r.checkForLostPackets()
}

// processAckQueue ACK 큐 처리
func (r *SRTReliability) processAckQueue() {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	
	for r.ackQueue.Len() > 0 {
		element := r.ackQueue.Front()
		seqNum := element.Value.(uint32)
		r.ackQueue.Remove(element)
		
		// ACK 패킷 전송
		ackPacket := CreateAckPacket(r.session.socketID, seqNum)
		if err := r.session.server.SendToClient(r.session.remoteAddr, ackPacket); err != nil {
			slog.Error("Failed to send ACK", 
				"socketID", r.session.socketID, 
				"ackSeqNum", seqNum, 
				"err", err)
		} else {
			r.statsMutex.Lock()
			r.stats.AcksSent++
			r.statsMutex.Unlock()
			
			slog.Debug("ACK sent", 
				"socketID", r.session.socketID, 
				"ackSeqNum", seqNum)
		}
	}
}

// processNakQueue NAK 큐 처리
func (r *SRTReliability) processNakQueue() {
	r.queueMutex.Lock()
	defer r.queueMutex.Unlock()
	
	for r.nakQueue.Len() > 0 {
		element := r.nakQueue.Front()
		seqNum := element.Value.(uint32)
		r.nakQueue.Remove(element)
		
		// NAK 패킷 전송
		nakPacket := CreateNakPacket(r.session.socketID, seqNum)
		if err := r.session.server.SendToClient(r.session.remoteAddr, nakPacket); err != nil {
			slog.Error("Failed to send NAK", 
				"socketID", r.session.socketID, 
				"nakSeqNum", seqNum, 
				"err", err)
		} else {
			r.statsMutex.Lock()
			r.stats.NaksSent++
			r.statsMutex.Unlock()
			
			slog.Debug("NAK sent", 
				"socketID", r.session.socketID, 
				"nakSeqNum", seqNum)
		}
	}
}

// processRetransmitQueue 재전송 큐 처리
func (r *SRTReliability) processRetransmitQueue() {
	r.retransmitMutex.Lock()
	defer r.retransmitMutex.Unlock()
	
	for r.retransmitQueue.Len() > 0 {
		element := r.retransmitQueue.Front()
		seqNum := element.Value.(uint32)
		r.retransmitQueue.Remove(element)
		
		r.sendBufferMutex.Lock()
		sentPacket, exists := r.sendBuffer[seqNum]
		if !exists {
			r.sendBufferMutex.Unlock()
			continue
		}
		
		// 재전송 간격 확인
		if time.Since(sentPacket.LastRetry) < r.config.RetryInterval {
			// 아직 재전송할 시간이 아님
			r.retransmitQueue.PushBack(seqNum)
			r.sendBufferMutex.Unlock()
			continue
		}
		
		sentPacket.RetryCount++
		sentPacket.LastRetry = time.Now()
		sentPacket.State = PacketStateRetransmitted
		r.sendBufferMutex.Unlock()
		
		// 패킷 재전송
		err := r.session.sendDataPacket(sentPacket.Data)
		if err != nil {
			slog.Error("Failed to retransmit packet", 
				"socketID", r.session.socketID, 
				"seqNum", seqNum, 
				"err", err)
		} else {
			r.statsMutex.Lock()
			r.stats.PacketsRetransmitted++
			r.statsMutex.Unlock()
			
			slog.Debug("Packet retransmitted", 
				"socketID", r.session.socketID, 
				"seqNum", seqNum, 
				"retryCount", sentPacket.RetryCount)
		}
	}
}

// checkForLostPackets 손실된 패킷들 확인 및 NAK 전송
func (r *SRTReliability) checkForLostPackets() {
	r.lossDetection.mutex.Lock()
	defer r.lossDetection.mutex.Unlock()
	
	now := time.Now()
	for seqNum, detectedTime := range r.lossDetection.missingPackets {
		if now.Sub(detectedTime) > r.lossDetection.detectionTime {
			// 패킷이 확실히 손실된 것으로 판단
			r.receiveBufferMutex.RLock()
			_, received := r.receiveBuffer[seqNum]
			r.receiveBufferMutex.RUnlock()
			
			if !received {
				// NAK 전송
				r.scheduleNak(seqNum)
				
				r.statsMutex.Lock()
				r.stats.PacketsLost++
				r.statsMutex.Unlock()
				
				slog.Debug("Packet confirmed lost, NAK scheduled", 
					"socketID", r.session.socketID, 
					"seqNum", seqNum)
			}
			
			delete(r.lossDetection.missingPackets, seqNum)
		}
	}
}

// cleanupOldPackets 오래된 패킷들 정리
func (r *SRTReliability) cleanupOldPackets() {
	now := time.Now()
	cleanupTime := 10 * time.Second
	
	for seqNum, packet := range r.sendBuffer {
		if now.Sub(packet.SentTime) > cleanupTime {
			delete(r.sendBuffer, seqNum)
		}
	}
}

// GetStats 신뢰성 통계 반환
func (r *SRTReliability) GetStats() ReliabilityStats {
	r.statsMutex.RLock()
	defer r.statsMutex.RUnlock()
	return r.stats
}

// Close 신뢰성 관리자 종료
func (r *SRTReliability) Close() {
	r.sendBufferMutex.Lock()
	r.sendBuffer = make(map[uint32]*SentPacket)
	r.sendBufferMutex.Unlock()
	
	r.receiveBufferMutex.Lock()
	r.receiveBuffer = make(map[uint32]*ReceivedPacket)
	r.receiveBufferMutex.Unlock()
	
	r.queueMutex.Lock()
	r.ackQueue = list.New()
	r.nakQueue = list.New()
	r.queueMutex.Unlock()
	
	r.retransmitMutex.Lock()
	r.retransmitQueue = list.New()
	r.retransmitMutex.Unlock()
	
	r.lossDetection.mutex.Lock()
	r.lossDetection.missingPackets = make(map[uint32]time.Time)
	r.lossDetection.mutex.Unlock()
}