package media

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// Buffer sync.Pool 기반 메모리 풀링을 지원하는 바이트 버퍼
type Buffer struct {
	data   []byte
	pool   *sync.Pool
	refCnt int32 // atomic 참조 카운트
}

// Buffer 객체 풀
var bufferPool = sync.Pool{
	New: func() any { return new(Buffer) },
}

// 7단계 크기별 메모리 풀들
var (
	pool8B   = &sync.Pool{New: func() any { return make([]byte, 8) }}
	pool16B  = &sync.Pool{New: func() any { return make([]byte, 16) }}
	pool32B  = &sync.Pool{New: func() any { return make([]byte, 32) }}
	pool128B = &sync.Pool{New: func() any { return make([]byte, 128) }}
	pool1KB  = &sync.Pool{New: func() any { return make([]byte, 1024) }}
	pool4KB  = &sync.Pool{New: func() any { return make([]byte, 4096) }}
	pool64KB = &sync.Pool{New: func() any { return make([]byte, 65536) }}
)

// pickDataPool 크기에 맞는 풀과 데이터를 선택
func pickDataPool(size int) (*sync.Pool, []byte) {
	switch {
	case size <= 8:
		return pool8B, pool8B.Get().([]byte)[:size]
	case size <= 16:
		return pool16B, pool16B.Get().([]byte)[:size]
	case size <= 32:
		return pool32B, pool32B.Get().([]byte)[:size]
	case size <= 128:
		return pool128B, pool128B.Get().([]byte)[:size]
	case size <= 1024:
		return pool1KB, pool1KB.Get().([]byte)[:size]
	case size <= 4096:
		return pool4KB, pool4KB.Get().([]byte)[:size]
	case size <= 65536:
		return pool64KB, pool64KB.Get().([]byte)[:size]
	default:
		// 큰 사이즈는 풀링하지 않고 직접 할당
		slog.Warn("Large buffer allocation without pooling", "size", size)
		return nil, make([]byte, size)
	}
}

// NewBuffer 지정된 크기에 맞는 풀에서 버퍼 할당 (객체 풀링 적용)
func NewBuffer(size int) *Buffer {
	// 객체 풀에서 Buffer 가져오기
	b := bufferPool.Get().(*Buffer)
	
	// 데이터 풀에서 적절한 크기 할당
	b.pool, b.data = pickDataPool(size)
	b.refCnt = 1 // 초기 참조 카운트
	
	return b
}

// Data 버퍼의 바이트 슬라이스 반환
func (b *Buffer) Data() []byte {
	return b.data
}

// Len 버퍼 길이 반환
func (b *Buffer) Len() int {
	return len(b.data)
}

// AddRef 참조 카운트를 증가시키고 자신을 반환 (멀티 컨슈머용)
func (b *Buffer) AddRef() *Buffer {
	atomic.AddInt32(&b.refCnt, 1)
	return b
}

// Release 참조 카운트를 감소시키고, 0이 되면 풀에 반납
func (b *Buffer) Release() {
	if atomic.AddInt32(&b.refCnt, -1) == 0 {
		b.returnToPool()
	}
}

// returnToPool 버퍼를 풀에 반납 (데이터 풀 + 객체 풀)
func (b *Buffer) returnToPool() {
	// 데이터를 데이터 풀에 반납
	if b.pool != nil {
		original := b.data[:cap(b.data)]
		b.pool.Put(original)
	}
	
	// Buffer 객체를 객체 풀에 반납
	bufferPool.Put(b)
}

// RefCount 현재 참조 카운트 반환 (디버깅용)
func (b *Buffer) RefCount() int32 {
	return atomic.LoadInt32(&b.refCnt)
}
