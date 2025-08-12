package srt

import (
	"runtime"
	"sync"
	"time"
)

// ObjectPool 재사용 가능한 객체 풀
type ObjectPool struct {
	packetPool   *sync.Pool
	bufferPool   *sync.Pool
	sessionPool  *sync.Pool
	framePool    *sync.Pool
}

// PerformanceMonitor 성능 모니터링
type PerformanceMonitor struct {
	// 메모리 통계
	allocatedMemory   uint64
	gcCount          uint32
	lastGCTime       time.Time
	maxMemoryUsage   uint64
	
	// 고루틴 통계  
	goroutineCount   int
	maxGoroutines    int
	
	// 처리량 통계
	packetsPerSecond uint64
	bytesPerSecond   uint64
	
	// 지연시간 통계
	avgProcessingTime time.Duration
	maxProcessingTime time.Duration
	
	mutex sync.RWMutex
}

// NewObjectPool 새 객체 풀 생성
func NewObjectPool() *ObjectPool {
	return &ObjectPool{
		packetPool: &sync.Pool{
			New: func() interface{} {
				return &UDTPacket{
					Payload: make([]byte, 0, MaxPayloadSize),
				}
			},
		},
		bufferPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, MaxPayloadSize)
			},
		},
		sessionPool: &sync.Pool{
			New: func() interface{} {
				return make(map[uint32]*Session)
			},
		},
		framePool: &sync.Pool{
			New: func() interface{} {
				return make([][]byte, 0, 10) // 평균 10개 청크
			},
		},
	}
}

// GetPacket 패킷 풀에서 패킷 가져오기
func (p *ObjectPool) GetPacket() *UDTPacket {
	packet := p.packetPool.Get().(*UDTPacket)
	// 패킷 초기화
	packet.Header = UDTHeader{}
	packet.Payload = packet.Payload[:0]
	return packet
}

// PutPacket 패킷을 풀에 반환
func (p *ObjectPool) PutPacket(packet *UDTPacket) {
	if packet != nil && cap(packet.Payload) <= MaxPayloadSize*2 {
		p.packetPool.Put(packet)
	}
}

// GetBuffer 버퍼 풀에서 버퍼 가져오기
func (p *ObjectPool) GetBuffer() []byte {
	return p.bufferPool.Get().([]byte)[:0]
}

// PutBuffer 버퍼를 풀에 반환
func (p *ObjectPool) PutBuffer(buffer []byte) {
	if cap(buffer) <= MaxPayloadSize*2 {
		p.bufferPool.Put(buffer)
	}
}

// GetSessionMap 세션 맵 가져오기
func (p *ObjectPool) GetSessionMap() map[uint32]*Session {
	sessionMap := p.sessionPool.Get().(map[uint32]*Session)
	// 맵 초기화 (기존 항목 제거)
	for k := range sessionMap {
		delete(sessionMap, k)
	}
	return sessionMap
}

// PutSessionMap 세션 맵 반환
func (p *ObjectPool) PutSessionMap(sessionMap map[uint32]*Session) {
	if len(sessionMap) < 1000 { // 너무 큰 맵은 반환하지 않음
		p.sessionPool.Put(sessionMap)
	}
}

// GetFrameSlice 프레임 슬라이스 가져오기
func (p *ObjectPool) GetFrameSlice() [][]byte {
	return p.framePool.Get().([][]byte)[:0]
}

// PutFrameSlice 프레임 슬라이스 반환
func (p *ObjectPool) PutFrameSlice(frames [][]byte) {
	if cap(frames) <= 50 { // 적정 크기만 재사용
		p.framePool.Put(frames)
	}
}

// NewPerformanceMonitor 새 성능 모니터 생성
func NewPerformanceMonitor() *PerformanceMonitor {
	return &PerformanceMonitor{
		lastGCTime: time.Now(),
	}
}

// UpdateMemoryStats 메모리 통계 업데이트
func (m *PerformanceMonitor) UpdateMemoryStats() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	m.mutex.Lock()
	m.allocatedMemory = memStats.Alloc
	m.gcCount = memStats.NumGC
	if memStats.Alloc > m.maxMemoryUsage {
		m.maxMemoryUsage = memStats.Alloc
	}
	m.mutex.Unlock()
}

// UpdateGoroutineStats 고루틴 통계 업데이트
func (m *PerformanceMonitor) UpdateGoroutineStats() {
	goroutines := runtime.NumGoroutine()
	
	m.mutex.Lock()
	m.goroutineCount = goroutines
	if goroutines > m.maxGoroutines {
		m.maxGoroutines = goroutines
	}
	m.mutex.Unlock()
}

// RecordProcessingTime 처리 시간 기록
func (m *PerformanceMonitor) RecordProcessingTime(duration time.Duration) {
	m.mutex.Lock()
	if duration > m.maxProcessingTime {
		m.maxProcessingTime = duration
	}
	// 간단한 이동 평균 계산
	m.avgProcessingTime = (m.avgProcessingTime + duration) / 2
	m.mutex.Unlock()
}

// RecordThroughput 처리량 기록
func (m *PerformanceMonitor) RecordThroughput(packets, bytes uint64) {
	m.mutex.Lock()
	m.packetsPerSecond = packets
	m.bytesPerSecond = bytes
	m.mutex.Unlock()
}

// GetStats 성능 통계 반환
func (m *PerformanceMonitor) GetStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	return map[string]interface{}{
		"allocated_memory_mb":    m.allocatedMemory / 1024 / 1024,
		"max_memory_usage_mb":    m.maxMemoryUsage / 1024 / 1024,
		"gc_count":              m.gcCount,
		"goroutine_count":       m.goroutineCount,
		"max_goroutines":        m.maxGoroutines,
		"packets_per_second":    m.packetsPerSecond,
		"bytes_per_second":      m.bytesPerSecond,
		"avg_processing_time_ms": m.avgProcessingTime.Milliseconds(),
		"max_processing_time_ms": m.maxProcessingTime.Milliseconds(),
	}
}

// ForceGC 강제 가비지 컬렉션
func (m *PerformanceMonitor) ForceGC() {
	runtime.GC()
	m.UpdateMemoryStats()
}

// WorkerPool 워커 풀로 고루틴 수 제한
type WorkerPool struct {
	workers    chan chan func()
	jobQueue   chan func()
	workerList []*Worker
	workerCount int
	quit       chan bool
	wg         sync.WaitGroup
}

// NewWorkerPool 새 워커 풀 생성
func NewWorkerPool(workerCount int) *WorkerPool {
	pool := &WorkerPool{
		workers:     make(chan chan func(), workerCount),
		jobQueue:    make(chan func(), workerCount*2),
		workerCount: workerCount,
		quit:        make(chan bool),
	}
	
	pool.start()
	return pool
}

// start 워커 풀 시작
func (p *WorkerPool) start() {
	p.workerList = make([]*Worker, p.workerCount)
	
	for i := 0; i < p.workerCount; i++ {
		worker := &Worker{
			id:       i,
			jobChan:  make(chan func()),
			workers:  p.workers,
			quit:     make(chan bool),
		}
		p.workerList[i] = worker
		worker.start(&p.wg)
	}
	
	// 작업 배분 고루틴
	go p.dispatch()
}

// Submit 작업 제출
func (p *WorkerPool) Submit(job func()) {
	select {
	case p.jobQueue <- job:
	default:
		// 큐가 가득 찬 경우 즉시 실행
		go job()
	}
}

// dispatch 작업 배분
func (p *WorkerPool) dispatch() {
	for {
		select {
		case job := <-p.jobQueue:
			workerJobChan := <-p.workers
			workerJobChan <- job
		case <-p.quit:
			return
		}
	}
}

// Stop 워커 풀 중지
func (p *WorkerPool) Stop() {
	close(p.quit)
	
	// 모든 워커에게 종료 신호 전송
	for _, worker := range p.workerList {
		close(worker.quit)
	}
	
	p.wg.Wait()
}

// Worker 개별 워커
type Worker struct {
	id      int
	jobChan chan func()
	workers chan chan func()
	quit    chan bool
}

// start 워커 시작
func (w *Worker) start(wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			// 워커를 풀에 등록
			w.workers <- w.jobChan
			
			select {
			case job := <-w.jobChan:
				// 작업 실행
				job()
			case <-w.quit:
				return
			}
		}
	}()
}

// CircularBuffer 순환 버퍼 (메모리 효율적인 링 버퍼)
type CircularBuffer struct {
	buffer   [][]byte
	head     int
	tail     int
	size     int
	capacity int
	mutex    sync.RWMutex
}

// NewCircularBuffer 새 순환 버퍼 생성
func NewCircularBuffer(capacity int) *CircularBuffer {
	return &CircularBuffer{
		buffer:   make([][]byte, capacity),
		capacity: capacity,
	}
}

// Push 데이터 추가
func (cb *CircularBuffer) Push(data []byte) bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	if cb.size == cb.capacity {
		return false // 버퍼 가득 참
	}
	
	// 데이터 복사
	cb.buffer[cb.tail] = make([]byte, len(data))
	copy(cb.buffer[cb.tail], data)
	
	cb.tail = (cb.tail + 1) % cb.capacity
	cb.size++
	return true
}

// Pop 데이터 제거 및 반환
func (cb *CircularBuffer) Pop() ([]byte, bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	if cb.size == 0 {
		return nil, false
	}
	
	data := cb.buffer[cb.head]
	cb.buffer[cb.head] = nil // GC 도움
	cb.head = (cb.head + 1) % cb.capacity
	cb.size--
	
	return data, true
}

// Len 버퍼 크기 반환
func (cb *CircularBuffer) Len() int {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.size
}

// IsFull 버퍼 가득 찬지 확인
func (cb *CircularBuffer) IsFull() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.size == cb.capacity
}

// IsEmpty 버퍼 비어있는지 확인
func (cb *CircularBuffer) IsEmpty() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.size == 0
}

// Clear 버퍼 초기화
func (cb *CircularBuffer) Clear() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	for i := 0; i < cb.capacity; i++ {
		cb.buffer[i] = nil
	}
	cb.head = 0
	cb.tail = 0
	cb.size = 0
}

// MemoryLimitChecker 메모리 사용량 체크
type MemoryLimitChecker struct {
	maxMemoryMB uint64
	checkInterval time.Duration
	onLimitExceeded func()
	quit chan bool
}

// NewMemoryLimitChecker 새 메모리 체커 생성
func NewMemoryLimitChecker(maxMemoryMB uint64, checkInterval time.Duration, callback func()) *MemoryLimitChecker {
	checker := &MemoryLimitChecker{
		maxMemoryMB: maxMemoryMB,
		checkInterval: checkInterval,
		onLimitExceeded: callback,
		quit: make(chan bool),
	}
	
	go checker.start()
	return checker
}

// start 메모리 체킹 시작
func (c *MemoryLimitChecker) start() {
	ticker := time.NewTicker(c.checkInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			
			currentMemoryMB := memStats.Alloc / 1024 / 1024
			if currentMemoryMB > c.maxMemoryMB {
				if c.onLimitExceeded != nil {
					c.onLimitExceeded()
				}
			}
		case <-c.quit:
			return
		}
	}
}

// Stop 메모리 체킹 중지
func (c *MemoryLimitChecker) Stop() {
	close(c.quit)
}

// ConnectionLimiter 연결 수 제한
type ConnectionLimiter struct {
	maxConnections int
	current        int
	mutex          sync.RWMutex
}

// NewConnectionLimiter 새 연결 제한기 생성
func NewConnectionLimiter(maxConnections int) *ConnectionLimiter {
	return &ConnectionLimiter{
		maxConnections: maxConnections,
	}
}

// Acquire 연결 획득 시도
func (l *ConnectionLimiter) Acquire() bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	
	if l.current >= l.maxConnections {
		return false
	}
	
	l.current++
	return true
}

// Release 연결 해제
func (l *ConnectionLimiter) Release() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	
	if l.current > 0 {
		l.current--
	}
}

// GetCurrent 현재 연결 수 반환
func (l *ConnectionLimiter) GetCurrent() int {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.current
}

// GetMax 최대 연결 수 반환
func (l *ConnectionLimiter) GetMax() int {
	return l.maxConnections
}

// IsAtLimit 연결 한계에 도달했는지 확인
func (l *ConnectionLimiter) IsAtLimit() bool {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.current >= l.maxConnections
}