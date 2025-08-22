package media

import (
	"sync"
	"sync/atomic"
)

// Buffer sync.Pool 기반 메모리 풀링을 지원하는 바이트 버퍼
type Buffer struct {
	data   []byte
	pool   *sync.Pool
	refCnt int32 // atomic 참조 카운트
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

// GetBuffer 지정된 크기에 맞는 풀에서 버퍼 할당
func GetBuffer(size int) *Buffer {
	var pool *sync.Pool
	var data []byte

	switch {
	case size <= 8:
		pool = pool8B
		data = pool8B.Get().([]byte)[:size]
	case size <= 16:
		pool = pool16B
		data = pool16B.Get().([]byte)[:size]
	case size <= 32:
		pool = pool32B
		data = pool32B.Get().([]byte)[:size]
	case size <= 128:
		pool = pool128B
		data = pool128B.Get().([]byte)[:size]
	case size <= 1024:
		pool = pool1KB
		data = pool1KB.Get().([]byte)[:size]
	case size <= 4096:
		pool = pool4KB
		data = pool4KB.Get().([]byte)[:size]
	case size <= 65536:
		pool = pool64KB
		data = pool64KB.Get().([]byte)[:size]
	default:
		// 큰 사이즈는 풀링하지 않고 직접 할당
		data = make([]byte, size)
		pool = nil
	}

	return &Buffer{
		data:   data,
		pool:   pool,
		refCnt: 1, // 초기 참조 카운트
	}
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
		// 풀에 반납 (pool이 nil이면 GC가 처리)
		if b.pool != nil {
			original := b.data[:cap(b.data)]
			b.pool.Put(original)
		}
	}
}

// RefCount 현재 참조 카운트 반환 (디버깅용)
func (b *Buffer) RefCount() int32 {
	return atomic.LoadInt32(&b.refCnt)
}
