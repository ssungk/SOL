package rtmp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sol/pkg/amf"
)

type session struct {
	reader          *messageReader
	writer          *messageWriter
	conn            net.Conn
	externalChannel chan<- interface{}
	messageChannel  chan *Message
}

func newSession(conn net.Conn) *session {
	s := &session{
		reader:          newMessageReader(),
		writer:          newMessageWriter(),
		conn:            conn,
		externalChannel: make(chan interface{}),
		messageChannel:  make(chan *Message, 10),
	}

	go s.handleRead()
	go s.handleEvent()

	return s
}

func (s *session) handleRead() {
	defer closeWithLog(s.conn)
	if err := handshake(s.conn); err != nil {
		slog.Info("Handshake failed:", "err", err)
		return
	}

	slog.Info("Handshake successful with", "addr", s.conn.RemoteAddr())

	for {
		slog.Info("loop")
		message, err := s.reader.readNextMessage(s.conn)
		if err != nil {
			return
		}

		switch message.messageHeader.typeId {
		case 1: // Set Chunk Size
			s.handleSetChunkSize(message)
		default:
			s.handleMessage(message)
			//s.messageChannel <- message
		}
	}
}

func (s *session) handleEvent() {
	for {
		select {
		case message := <-s.messageChannel:
			s.handleMessage(message)
		}
	}
}

func (s *session) handleMessage(message *Message) {
	slog.Info("receive message", "typeId", message.messageHeader.typeId)
	switch message.messageHeader.typeId {
	case 1: // Set Chunk Size
		s.handleSetChunkSize(message)
	case 2: // Abort Message
		// Optional: ignore or log
	case 3: // Acknowledgement
		// 서버용: 클라이언트의 ack 수신
	case 4: // User Control Messages
		//s.handleUserControl(message)
	case 5: // Window Acknowledgement Size
		// 클라이언트가 설정한 ack 윈도우 크기
	case 6: // Set Peer Bandwidth
		// bandwidth 제한에 대한 정보
	case 8: // Audio
		//s.handleAudio(message)
	case 9: // Video
		//s.handleVideo(message)
	case 15: // AMF3 Data Message
		// AMF3 포맷. 대부분 Flash Player
	case 16: // AMF3 Shared Object
	case 17: // AMF3 Command Message
	case 18: // AMF0 Data Message (e.g., onMetaData)
		//s.handleScriptData(message)
	case 20: // AMF0 Command Message (e.g., connect, play, publish)
		s.handleAMF0Command(message)
	default:
		slog.Warn("unhandled RTMP message type", "type", message.messageHeader.typeId)
	}
}

func (s *session) handleSetChunkSize(message *Message) {
	slog.Info("handleSetChunkSize")

	if len(message.payload[0]) != 4 {
		slog.Error("Invalid Set Chunk Size message length", "length", len(message.payload))
		return
	}

	// 4바이트에서 uint32로 읽기 (big endian)
	newChunkSize := binary.BigEndian.Uint32(message.payload[0])

	// 첫 번째 비트(최상위 비트) 체크: 반드시 0이어야 함
	if newChunkSize&0x80000000 != 0 {
		slog.Error("Set Chunk Size has reserved highest bit set", "value", newChunkSize)
		// TODO: 에러 처리후 연결 종료??
		return
	}

	// RTMP 최대 청크 크기 제한 (1 ~ 16777215)
	const maxChunkSize = 0xFFFFFF
	if newChunkSize < 1 || newChunkSize > maxChunkSize {
		slog.Error("Set Chunk Size out of valid range", "value", newChunkSize)
		return
	}

	// 실제 세션 청크 크기 적용
	s.reader.setChunkSize(newChunkSize)
}

func (s *session) handleAMF0Command(message *Message) {
	slog.Info("handleAMF0Command")
	reader := ConcatByteSlicesReader(message.payload)
	values, err := amf.DecodeAMF0Sequence(reader)
	if err != nil {
		// TODO: handle error
	}
	for _, v := range values {
		slog.Info("amf", "value", v)
	}

	commandName, ok := values[0].(string)
	if !ok {
		slog.Error("Invalid command name type", "actual", fmt.Sprintf("%T", values[0]))
		return
	}

	switch commandName {
	case "connect":
		s.handleConnect(values)
	case "createStream":
		//s.handleCreateStream(values)
	case "publish":
		//s.handlePublish(values)
	case "play":
		//s.handlePlay(values)
	case "pause":
		//s.handlePause(values)
	case "deleteStream":
		//s.handleDeleteStream(values)
	case "closeStream":
		//s.handleCloseStream(values)
	case "releaseStream":
		//s.handleReleaseStream(values)
	case "FCPublish":
		//s.handleFCPublish(values)
	case "receiveAudio":
		//s.handleReceiveAudio(values)
	case "receiveVideo":
		//s.handleReceiveVideo(values)
	case "onBWDone":
		//s.handleOnBWDone(values)
	default:
		slog.Error("Unknown AMF0 command", "name", commandName)
	}
}

func (s *session) handleConnect(values []any) {
	slog.Info("handling connect", "params", values)

	// 최소 3개 요소: "connect", transaction ID, command object
	if len(values) < 3 {
		slog.Error("connect: not enough parameters", "length", len(values))
		return
	}

	transactionID, ok := values[1].(float64)
	if !ok {
		slog.Error("connect: invalid transaction ID", "type", fmt.Sprintf("%T", values[1]))
		return
	}

	slog.Info("handling connect", "transactionID", transactionID)

	// command object (map)
	commandObj, ok := values[2].(map[string]any)
	if !ok {
		slog.Error("connect: invalid command object", "type", fmt.Sprintf("%T", values[2]))
		return
	}

	slog.Info("object", "commandObj", commandObj)

	obj := map[string]any{
		"level":          "status",
		"code":           "NetConnection.Connect.Success",
		"description":    "Connection succeeded.",
		"objectEncoding": 0,
	}

	sequence, err := amf.EncodeAMF0Sequence("_result", transactionID, nil, obj)
	if err != nil {
		return
	}

	slog.Info("encoded _result sequence", "sequence", sequence)
	err = s.writer.writeSetChunkSize(s.conn, 4096)
	if err != nil {
		return
	}

	err = s.writer.writeCommand(s.conn, sequence)
	if err != nil {
		return
	}

}

func ConcatByteSlicesReader(slices [][]byte) io.Reader {
	readers := make([]io.Reader, 0, len(slices))
	for _, b := range slices {
		readers = append(readers, bytes.NewReader(b))
	}
	return io.MultiReader(readers...)
}
