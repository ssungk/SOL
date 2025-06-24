package rtmp

import (
	"encoding/binary"
	"io"
)

type messageWriter struct {
	chunkSize uint32
}

func newMessageWriter() *messageWriter {
	return &messageWriter{
		chunkSize: 128,
	}
}

func (mw *messageWriter) write(w io.Writer, payload []byte) error {
	//?????
	return nil
}

func (mw *messageWriter) writeCommand(w io.Writer, payload []byte) error {
	const (
		chunkStreamID   = 3
		messageTypeID   = 20 // AMF0 Command Message
		messageStreamID = 0
		timestamp       = 0
	)

	// === Basic Header ===
	// fmt=0 (2 bits), csid=3 (6 bits) → 0x03
	basicHeader := []byte{0x03}
	if _, err := w.Write(basicHeader); err != nil {
		return err
	}

	// === Message Header ===
	header := make([]byte, 11)
	PutUint24(header[0:], uint32(timestamp))                   // 3 bytes timestamp (big endian)
	PutUint24(header[3:], uint32(len(payload)))                // 3 bytes message length (big endian)
	header[6] = messageTypeID                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], messageStreamID) // 4 bytes message stream ID (little endian)

	if _, err := w.Write(header); err != nil {
		return err
	}

	// === Payload ===
	if _, err := w.Write(payload); err != nil {
		return err
	}

	return nil
}

func (mw *messageWriter) writeSetChunkSize(w io.Writer, chunkSize uint32) error {
	const (
		fmtType         = 0 // full 12-byte header
		chunkStreamID   = 2 // 관례적으로 2 사용
		messageTypeID   = 1 // Set Chunk Size
		messageStreamID = 0 // 항상 0
		timestamp       = 0
	)

	// === Basic Header ===
	basicHeader := []byte{(fmtType << 6) | byte(chunkStreamID)} // fmt=0, csid=2 → 0x02
	if _, err := w.Write(basicHeader); err != nil {
		return err
	}

	// === Message Header ===
	header := make([]byte, 11)
	PutUint24(header[0:], timestamp) // 3-byte timestamp
	PutUint24(header[3:], 4)         // 3-byte payload length
	header[6] = messageTypeID        // type = 1 (Set Chunk Size)
	binary.LittleEndian.PutUint32(header[7:], messageStreamID)
	if _, err := w.Write(header); err != nil {
		return err
	}

	// === Payload ===
	if err := binary.Write(w, binary.BigEndian, chunkSize); err != nil {
		return err
	}

	return nil
}

func PutUint24(b []byte, v uint32) {
	b[0] = byte((v >> 16) & 0xFF)
	b[1] = byte((v >> 8) & 0xFF)
	b[2] = byte(v & 0xFF)
}
