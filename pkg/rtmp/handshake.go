package rtmp

import (
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
)

// ServerHandshake 서버측 RTMP handshake 수행
func ServerHandshake(rw io.ReadWriter) error {
	slog.Debug("Starting server handshake")
	
	// C0 수신 및 검증
	c0 := make([]byte, 1)
	if _, err := io.ReadFull(rw, c0); err != nil {
		return fmt.Errorf("failed to read C0: %w", err)
	}
	if c0[0] != RTMPVersion {
		return fmt.Errorf("unsupported RTMP version: %d", c0[0])
	}

	// S0 전송
	if _, err := rw.Write(c0); err != nil {
		return fmt.Errorf("failed to write S0: %w", err)
	}

	// S1 생성 및 전송
	s1 := make([]byte, HandshakeSize)
	copy(s1[0:4], []byte{0, 0, 0, 0}) // time field
	copy(s1[4:8], []byte{0, 0, 0, 0}) // zero field
	_, _ = rand.Read(s1[8:])          // random field
	if _, err := rw.Write(s1); err != nil {
		return fmt.Errorf("failed to write S1: %w", err)
	}

	// C1 수신
	c1 := make([]byte, HandshakeSize)
	if _, err := io.ReadFull(rw, c1); err != nil {
		return fmt.Errorf("failed to read C1: %w", err)
	}

	// S2 전송 (C1의 echo)
	if _, err := rw.Write(c1); err != nil {
		return fmt.Errorf("failed to write S2: %w", err)
	}

	// C2 수신
	c2 := make([]byte, HandshakeSize)
	if _, err := io.ReadFull(rw, c2); err != nil {
		return fmt.Errorf("failed to read C2: %w", err)
	}

	slog.Debug("Server handshake completed")
	return nil
}

// ClientHandshake 클라이언트측 RTMP handshake 수행
func ClientHandshake(rw io.ReadWriter) error {
	slog.Debug("Starting client handshake")

	// C0 전송
	c0 := []byte{RTMPVersion}
	if _, err := rw.Write(c0); err != nil {
		return fmt.Errorf("failed to write C0: %w", err)
	}

	// C1 생성 및 전송
	c1 := make([]byte, HandshakeSize)
	copy(c1[0:4], []byte{0, 0, 0, 0}) // time field
	copy(c1[4:8], []byte{0, 0, 0, 0}) // zero field
	_, _ = rand.Read(c1[8:])          // random field
	if _, err := rw.Write(c1); err != nil {
		return fmt.Errorf("failed to write C1: %w", err)
	}

	// S0 수신 및 검증
	s0 := make([]byte, 1)
	if _, err := io.ReadFull(rw, s0); err != nil {
		return fmt.Errorf("failed to read S0: %w", err)
	}
	if s0[0] != RTMPVersion {
		return fmt.Errorf("unsupported RTMP version from server: %d", s0[0])
	}

	// S1 수신
	s1 := make([]byte, HandshakeSize)
	if _, err := io.ReadFull(rw, s1); err != nil {
		return fmt.Errorf("failed to read S1: %w", err)
	}

	// C2 전송 (S1의 echo)
	if _, err := rw.Write(s1); err != nil {
		return fmt.Errorf("failed to write C2: %w", err)
	}

	// S2 수신
	s2 := make([]byte, HandshakeSize)
	if _, err := io.ReadFull(rw, s2); err != nil {
		return fmt.Errorf("failed to read S2: %w", err)
	}

	slog.Debug("Client handshake completed")
	return nil
}

