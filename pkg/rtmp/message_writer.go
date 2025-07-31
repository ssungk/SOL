package rtmp

import (
	"encoding/binary"
	"io"
	"sol/pkg/amf"
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
	// 향후 확장용
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

	// === Payload (청킹 적용) ===
	return mw.writeChunkedData(w, payload, chunkStreamID)
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

	mw.chunkSize = chunkSize

	// Set Chunk Size는 작은 메시지이므로 청킹 불필요
	return nil
}

func PutUint24(b []byte, v uint32) {
	b[0] = byte((v >> 16) & 0xFF)
	b[1] = byte((v >> 8) & 0xFF)
	b[2] = byte(v & 0xFF)
}

// 오디오 데이터 전송
func (mw *messageWriter) writeAudioData(w io.Writer, audioData []byte, timestamp uint32) error {
	const (
		fmtType         = 0 // full 12-byte header
		chunkStreamID   = 4 // 오디오용 chunk stream ID
		messageTypeID   = 8 // Audio
		messageStreamID = 0 // stream ID (일관성 위해 0으로 통일)
	)

	// 임시로 확장 타임스탬프 비활성화 (24비트로 제한)
	headerTimestamp := timestamp
	if timestamp >= 0xFFFFFF {
		headerTimestamp = 0xFFFFFF - 1 // 임시 방안
	}

	// Basic Header
	basicHeader := []byte{(fmtType << 6) | byte(chunkStreamID)}
	if _, err := w.Write(basicHeader); err != nil {
		return err
	}

	// Message Header
	header := make([]byte, 11)
	PutUint24(header[0:], headerTimestamp)                     // 3 bytes timestamp
	PutUint24(header[3:], uint32(len(audioData)))              // 3 bytes message length
	header[6] = messageTypeID                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], messageStreamID) // 4 bytes message stream ID

	if _, err := w.Write(header); err != nil {
		return err
	}

	// 오디오 데이터를 청크 단위로 전송
	return mw.writeChunkedData(w, audioData, chunkStreamID)
}

// 비디오 데이터 전송
func (mw *messageWriter) writeVideoData(w io.Writer, videoData []byte, timestamp uint32) error {
	const (
		fmtType         = 0 // full 12-byte header
		chunkStreamID   = 5 // 비디오용 chunk stream ID
		messageTypeID   = 9 // Video
		messageStreamID = 0 // stream ID (일관성 위해 0으로 통일)
	)

	// 임시로 확장 타임스탬프 비활성화 (24비트로 제한)
	headerTimestamp := timestamp
	if timestamp >= 0xFFFFFF {
		headerTimestamp = 0xFFFFFF - 1 // 임시 방안
	}

	// Basic Header
	basicHeader := []byte{(fmtType << 6) | byte(chunkStreamID)}
	if _, err := w.Write(basicHeader); err != nil {
		return err
	}

	// Message Header
	header := make([]byte, 11)
	PutUint24(header[0:], headerTimestamp)                     // 3 bytes timestamp
	PutUint24(header[3:], uint32(len(videoData)))              // 3 bytes message length
	header[6] = messageTypeID                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], messageStreamID) // 4 bytes message stream ID

	if _, err := w.Write(header); err != nil {
		return err
	}

	// 비디오 데이터를 청크 단위로 전송
	return mw.writeChunkedData(w, videoData, chunkStreamID)
}

// 메타데이터 전송
func (mw *messageWriter) writeScriptData(w io.Writer, commandName string, metadata map[string]any) error {
	const (
		fmtType         = 0  // full 12-byte header
		chunkStreamID   = 6  // 스크립트 데이터용 chunk stream ID
		messageTypeID   = 18 // AMF0 Data Message
		messageStreamID = 0  // stream ID (일관성 위해 0으로 통일)
		timestamp       = 0  // 메타데이터는 timestamp 0
	)

	// AMF 데이터 인코딩
	payload, err := amf.EncodeAMF0Sequence(commandName, metadata)
	if err != nil {
		return err
	}

	// Basic Header
	basicHeader := []byte{(fmtType << 6) | byte(chunkStreamID)}
	if _, err := w.Write(basicHeader); err != nil {
		return err
	}

	// Message Header
	header := make([]byte, 11)
	PutUint24(header[0:], timestamp)                           // 3 bytes timestamp
	PutUint24(header[3:], uint32(len(payload)))                // 3 bytes message length
	header[6] = messageTypeID                                  // 1 byte type ID
	binary.LittleEndian.PutUint32(header[7:], messageStreamID) // 4 bytes message stream ID

	if _, err := w.Write(header); err != nil {
		return err
	}

	// 메타데이터를 청크 단위로 전송
	return mw.writeChunkedData(w, payload, chunkStreamID)
}

// 데이터를 청크 단위로 전송 (RTMP 표준에 따라)
func (mw *messageWriter) writeChunkedData(w io.Writer, data []byte, chunkStreamID byte) error {
	dataLength := len(data)

	// 데이터가 청크 크기보다 작거나 같으면 한 번에 전송
	if dataLength <= int(mw.chunkSize) {
		_, err := w.Write(data)
		return err
	}

	// 데이터가 청크 크기보다 크면 여러 청크로 분할
	bytesRemaining := dataLength
	offset := 0

	for bytesRemaining > 0 {
		chunkSize := int(mw.chunkSize)
		if bytesRemaining < chunkSize {
			chunkSize = bytesRemaining
		}

		// 첫 번째 청크는 이미 헤더가 전송됨, 두 번째부터 Type 3 헤더 필요
		if offset > 0 {
			// Type 3: 연속 청크 헤더 (fmt=3, csid=chunkStreamID)
			type3Header := []byte{(3 << 6) | chunkStreamID}
			if _, err := w.Write(type3Header); err != nil {
				return err
			}
		}

		// 청크 데이터 전송
		if _, err := w.Write(data[offset : offset+chunkSize]); err != nil {
			return err
		}

		offset += chunkSize
		bytesRemaining -= chunkSize
	}

	return nil
}
