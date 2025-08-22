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

// 다양한 크기별 메모리 풀들
var (
	pool1KB  = &sync.Pool{New: func() any { return make([]byte, 1024) }}
	pool4KB  = &sync.Pool{New: func() any { return make([]byte, 4096) }}
	pool16KB = &sync.Pool{New: func() any { return make([]byte, 16384) }}
	pool64KB = &sync.Pool{New: func() any { return make([]byte, 65536) }}
)

// GetBuffer 지정된 크기에 맞는 풀에서 버퍼 할당
func GetBuffer(size int) *Buffer {
	var pool *sync.Pool
	var data []byte

	switch {
	case size <= 1024:
		pool = pool1KB
		data = pool1KB.Get().([]byte)[:size]
	case size <= 4096:
		pool = pool4KB
		data = pool4KB.Get().([]byte)[:size]
	case size <= 16384:
		pool = pool16KB
		data = pool16KB.Get().([]byte)[:size]
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
	if b == nil {
		return nil
	}
	return b.data
}

// Len 버퍼 길이 반환
func (b *Buffer) Len() int {
	if b == nil {
		return 0
	}
	return len(b.data)
}

// AddRef 참조 카운트를 증가시키고 자신을 반환 (멀티 컨슈머용)
func (b *Buffer) AddRef() *Buffer {
	if b == nil {
		return nil
	}
	atomic.AddInt32(&b.refCnt, 1)
	return b
}

// Release 참조 카운트를 감소시키고, 0이 되면 풀에 반납
func (b *Buffer) Release() {
	if b == nil {
		return
	}

	if atomic.AddInt32(&b.refCnt, -1) == 0 {
		// 참조 카운트가 0이 되면 풀에 반납
		if b.pool != nil {
			// 원본 크기로 복구하여 풀에 반납
			switch b.pool {
			case pool1KB:
				original := b.data[:cap(b.data)][:1024]
				b.pool.Put(original)
			case pool4KB:
				original := b.data[:cap(b.data)][:4096]
				b.pool.Put(original)
			case pool16KB:
				original := b.data[:cap(b.data)][:16384]
				b.pool.Put(original)
			case pool64KB:
				original := b.data[:cap(b.data)][:65536]
				b.pool.Put(original)
			}
		}
		// 큰 사이즈는 GC가 자동 처리
	}
}

// RefCount 현재 참조 카운트 반환 (디버깅용)
func (b *Buffer) RefCount() int32 {
	if b == nil {
		return 0
	}
	return atomic.LoadInt32(&b.refCnt)
}

// Copy 데이터를 복사하여 새로운 Buffer 생성
func (b *Buffer) Copy() *Buffer {
	if b == nil {
		return nil
	}
	
	newBuffer := GetBuffer(len(b.data))
	copy(newBuffer.data, b.data)
	return newBuffer
}