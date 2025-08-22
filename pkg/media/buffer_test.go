package media

import (
	"sync"
	"testing"
)

func TestGetBuffer(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"1KB pool", 512, 512},
		{"1KB pool max", 1024, 1024},
		{"4KB pool", 2048, 2048},
		{"4KB pool max", 4096, 4096},
		{"16KB pool", 8192, 8192},
		{"16KB pool max", 16384, 16384},
		{"64KB pool", 32768, 32768},
		{"64KB pool max", 65536, 65536},
		{"Large size", 100000, 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := GetBuffer(tt.size)
			if buf == nil {
				t.Fatal("GetBuffer returned nil")
			}

			if len(buf.Data()) != tt.expected {
				t.Errorf("Expected length %d, got %d", tt.expected, len(buf.Data()))
			}

			if buf.Len() != tt.expected {
				t.Errorf("Expected Len() %d, got %d", tt.expected, buf.Len())
			}

			// 참조 카운트 확인
			if buf.RefCount() != 1 {
				t.Errorf("Expected initial refCount 1, got %d", buf.RefCount())
			}

			buf.Release()
		})
	}
}

func TestBufferRefCounting(t *testing.T) {
	buf := GetBuffer(1024)

	// 초기 참조 카운트
	if buf.RefCount() != 1 {
		t.Errorf("Expected initial refCount 1, got %d", buf.RefCount())
	}

	// AddRef 테스트
	buf2 := buf.AddRef()
	if buf.RefCount() != 2 {
		t.Errorf("Expected refCount 2 after AddRef, got %d", buf.RefCount())
	}
	if buf2 != buf {
		t.Error("AddRef should return the same buffer")
	}

	// 첫 번째 Release
	buf.Release()
	if buf.RefCount() != 1 {
		t.Errorf("Expected refCount 1 after first Release, got %d", buf.RefCount())
	}

	// 두 번째 Release (풀에 반납됨)
	buf2.Release()
}

func TestBufferCopy(t *testing.T) {
	original := GetBuffer(1024)
	data := original.Data()
	
	// 테스트 데이터 설정
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Copy 테스트
	copied := original.Copy()
	if copied == nil {
		t.Fatal("Copy returned nil")
	}

	// 데이터 비교
	if len(copied.Data()) != len(original.Data()) {
		t.Errorf("Copy length mismatch: expected %d, got %d", len(original.Data()), len(copied.Data()))
	}

	for i, b := range original.Data() {
		if copied.Data()[i] != b {
			t.Errorf("Copy data mismatch at index %d: expected %d, got %d", i, b, copied.Data()[i])
		}
	}

	// 서로 다른 인스턴스인지 확인
	if copied == original {
		t.Error("Copy should return different instance")
	}

	// 참조 카운트 독립성 확인
	if copied.RefCount() != 1 {
		t.Errorf("Copy should have initial refCount 1, got %d", copied.RefCount())
	}

	original.Release()
	copied.Release()
}

func TestBufferNilSafety(t *testing.T) {
	var buf *Buffer

	// nil 버퍼에 대한 안전성 테스트
	if buf.Data() != nil {
		t.Error("nil buffer Data() should return nil")
	}

	if buf.Len() != 0 {
		t.Error("nil buffer Len() should return 0")
	}

	if buf.RefCount() != 0 {
		t.Error("nil buffer RefCount() should return 0")
	}

	if buf.AddRef() != nil {
		t.Error("nil buffer AddRef() should return nil")
	}

	if buf.Copy() != nil {
		t.Error("nil buffer Copy() should return nil")
	}

	// Release는 패닉 없이 처리되어야 함
	buf.Release() // should not panic
}

func TestBufferConcurrency(t *testing.T) {
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for j := 0; j < numOperations; j++ {
				// 다양한 크기로 테스트
				size := 512 + (j % 4096)
				buf := GetBuffer(size)
				
				// 데이터 쓰기
				data := buf.Data()
				for k := range data {
					data[k] = byte(k % 256)
				}
				
				// AddRef/Release 테스트
				if j%2 == 0 {
					buf2 := buf.AddRef()
					buf2.Release()
				}
				
				buf.Release()
			}
		}()
	}
	
	wg.Wait()
}

func BenchmarkGetBuffer1KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(1024)
		buf.Release()
	}
}

func BenchmarkGetBuffer4KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(4096)
		buf.Release()
	}
}

func BenchmarkMakeSlice1KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, 1024)
	}
}

func BenchmarkMakeSlice4KB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, 4096)
	}
}