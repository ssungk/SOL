package media

import (
	"sync"
	"unsafe"
)

// PooledBuffer represents a buffer allocated from a pool
type PooledBuffer struct {
	Data     []byte     // 실제 데이터 슬라이스
	pool     *sync.Pool // 원본 pool 참조
	original []byte     // 전체 용량을 가진 원본 버퍼 (반납용)
}

// PoolManager manages pooled buffers and their lifecycle
type PoolManager struct {
	buffers map[uintptr]*PooledBuffer // 버퍼 포인터로 PooledBuffer 추적
	mu      sync.RWMutex              // 동시성 제어
}

// AllocateBuffer allocates a buffer from the given pool and tracks it
func (pm *PoolManager) AllocateBuffer(pool *sync.Pool, size uint32) *PooledBuffer {
	// Pool에서 버퍼 할당
	buf := pool.Get().([]byte)[:size]
	
	pb := &PooledBuffer{
		Data:     buf,
		pool:     pool,
		original: buf[:cap(buf)], // 전체 용량 유지
	}
	
	// 추적을 위해 등록
	pm.mu.Lock()
	pm.buffers[uintptr(unsafe.Pointer(&pb.Data[0]))] = pb
	pm.mu.Unlock()
	
	return pb
}

// ReleaseBuffer releases a buffer back to its pool
func (pm *PoolManager) ReleaseBuffer(data []byte) {
	if len(data) == 0 {
		return
	}
	
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	// 데이터 포인터로 PooledBuffer 찾기
	key := uintptr(unsafe.Pointer(&data[0]))
	if pb, exists := pm.buffers[key]; exists {
		// Pool에 반납
		pb.pool.Put(pb.original)
		// 추적에서 제거
		delete(pm.buffers, key)
	}
}

// GetTrackedCount returns the number of currently tracked buffers
func (pm *PoolManager) GetTrackedCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.buffers)
}

// ManagedFrame represents a frame with pool-managed buffers
type ManagedFrame struct {
	Frame
	poolManager *PoolManager     // Pool manager 참조
	pooledData  []*PooledBuffer  // 추적되는 pooled buffers
}

// 생성자 함수들

// NewPoolManager creates a new pool manager
func NewPoolManager() *PoolManager {
	return &PoolManager{
		buffers: make(map[uintptr]*PooledBuffer),
	}
}

// NewManagedFrame creates a new managed frame
func NewManagedFrame(frameType Type, subType FrameSubType, timestamp uint32, poolManager *PoolManager) *ManagedFrame {
	return &ManagedFrame{
		Frame: Frame{
			Type:      frameType,
			SubType:   subType,
			Timestamp: timestamp,
			Data:      make([][]byte, 0),
		},
		poolManager: poolManager,
		pooledData:  make([]*PooledBuffer, 0),
	}
}

// AddPooledChunk adds a pooled buffer chunk to the frame
func (mf *ManagedFrame) AddPooledChunk(pb *PooledBuffer) {
	mf.Frame.Data = append(mf.Frame.Data, pb.Data)
	mf.pooledData = append(mf.pooledData, pb)
}

// AddRegularChunk adds a non-pooled chunk to the frame
func (mf *ManagedFrame) AddRegularChunk(data []byte) {
	mf.Frame.Data = append(mf.Frame.Data, data)
	mf.pooledData = append(mf.pooledData, nil) // nil로 표시
}

// Release releases all pooled buffers back to their pools
func (mf *ManagedFrame) Release() {
	for _, pb := range mf.pooledData {
		if pb != nil {
			pb.pool.Put(pb.original)
		}
	}
	mf.pooledData = nil
	mf.Frame.Data = nil
}

// ToRegularFrame converts managed frame to regular frame (copying data)
func (mf *ManagedFrame) ToRegularFrame() Frame {
	// 모든 데이터를 복사하여 새로운 Frame 생성
	newData := make([][]byte, len(mf.Frame.Data))
	for i, chunk := range mf.Frame.Data {
		newChunk := make([]byte, len(chunk))
		copy(newChunk, chunk)
		newData[i] = newChunk
	}
	
	return Frame{
		Type:      mf.Frame.Type,
		SubType:   mf.Frame.SubType,
		Timestamp: mf.Frame.Timestamp,
		Data:      newData,
	}
}

// Clone creates a copy of the managed frame (increments pool reference)
func (mf *ManagedFrame) Clone() *ManagedFrame {
	clone := &ManagedFrame{
		Frame: Frame{
			Type:      mf.Frame.Type,
			SubType:   mf.Frame.SubType,
			Timestamp: mf.Frame.Timestamp,
			Data:      make([][]byte, len(mf.Frame.Data)),
		},
		poolManager: mf.poolManager,
		pooledData:  make([]*PooledBuffer, len(mf.pooledData)),
	}
	
	// 데이터와 pool 참조 복사
	copy(clone.Frame.Data, mf.Frame.Data)
	copy(clone.pooledData, mf.pooledData)
	
	return clone
}