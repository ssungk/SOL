package rtmp

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

type testReadWriter struct {
	io.Reader
	io.Writer
}

func newTestReadWriter(r io.Reader, w io.Writer) *testReadWriter {
	return &testReadWriter{
		Reader: r,
		Writer: w,
	}
}

type failWriter struct {
	remainingBytes int
}

func newFailWriter(maxBytes int) *failWriter {
	return &failWriter{remainingBytes: maxBytes}
}

func (w *failWriter) Write(p []byte) (int, error) {
	if len(p) > w.remainingBytes {
		return 0, fmt.Errorf("write failed intentionally after exceeding max bytes")
	}
	w.remainingBytes -= len(p)
	return len(p), nil
}

func TestHandshake(t *testing.T) {
	data := append([]byte{0x03}, make([]byte, 1536*2)...)
	rw := newTestReadWriter(bytes.NewReader(data), io.Discard)
	err := handshake(rw)
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
}

func TestHandshakeFailReadC0(t *testing.T) {
	rw := newTestReadWriter(bytes.NewReader(nil), newFailWriter(0))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestHandshakeFailInvalidC0Version(t *testing.T) {
	data := []byte{0x02}
	rw := newTestReadWriter(bytes.NewReader(data), newFailWriter(0))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestHandshakeFailWriteS0(t *testing.T) {
	data := []byte{0x03}
	rw := newTestReadWriter(bytes.NewReader(data), newFailWriter(0))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestHandshakeFailWriteS1(t *testing.T) {
	data := []byte{0x03}
	rw := newTestReadWriter(bytes.NewReader(data), newFailWriter(1))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestHandshakeFailReadC1(t *testing.T) {
	data := []byte{0x03}
	rw := newTestReadWriter(bytes.NewReader(data), newFailWriter(1+1536))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestHandshakeFailWriteS2(t *testing.T) {
	data := append([]byte{0x03}, make([]byte, 1536)...)
	rw := newTestReadWriter(bytes.NewReader(data), newFailWriter(1+1536))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestHandshakeFailReadC2(t *testing.T) {
	data := append([]byte{0x03}, make([]byte, 1536)...)
	rw := newTestReadWriter(bytes.NewReader(data), newFailWriter(1+1536*2))
	err := handshake(rw)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}
