package rtmp

import (
	"fmt"
	"io"
	"log/slog"
	"net"
)

type Server struct {
	port    int
	channel chan interface{}
}

func NewServer() *Server {
	rtmp := &Server{
		port: 1935,
	}
	return rtmp
}

func (s *Server) Start() {
	slog.Debug("중간 처리 단계", "task", "progress", "50%")
	go s.ListenAndServe()
}

func (s *Server) Stop() {

}

func (s *Server) eventLoop() {
	for {
		select {
		case data := <-s.channel:
			s.channelHandler(data)
		}
	}
}

func (s *Server) channelHandler(data interface{}) {
	switch v := data.(type) {
	case Terminated:
		s.TerminatedEventHandler(v.Id)
	default: //logError("Received unknown data type")
	}
}

func (s *Server) TerminatedEventHandler(id string) {

}

func (s *Server) ListenAndServe() {
	ln, err := net.Listen("tcp", ":1935")
	if err != nil {
		fmt.Println("Error starting TCP server:", err)
		return
	}
	defer ln.Close()
	fmt.Println("TCP server listening on port 8080")

	for {
		// 클라이언트 연결 수신
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}

		// 클라이언트 연결을 처리하기 위해 고루틴 시작
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Println("Client connected:", conn.RemoteAddr())

	// RTMP 핸드셰이크 수행
	if err := performHandshake(conn); err != nil {
		fmt.Println("Handshake failed:", err)
		return
	}

	fmt.Println("Handshake successful with", conn.RemoteAddr())

	// RTMP 메시지 읽기
	readRTMPMessages(conn)
}

func performHandshake(conn net.Conn) error {
	// RTMP 핸드셰이크는 C0, C1, C2 메시지를 교환하는 과정입니다.
	// C0: 프로토콜 버전 (1바이트)
	// C1: 클라이언트 타임스탬프, 무작위 데이터 (1536바이트)
	// C2: 서버 타임스탬프, 무작위 데이터 (1536바이트)

	// C0 읽기
	c0 := make([]byte, 1)
	if _, err := conn.Read(c0); err != nil {
		return fmt.Errorf("failed to read C0: %w", err)
	}

	// C1 읽기
	c1 := make([]byte, 1536)
	if _, err := conn.Read(c1); err != nil {
		return fmt.Errorf("failed to read C1: %w", err)
	}

	// S0: C0와 동일하게 보냄
	if _, err := conn.Write(c0); err != nil {
		return fmt.Errorf("failed to write S0: %w", err)
	}

	// S1: 서버 타임스탬프와 무작위 데이터
	s1 := make([]byte, 1536)
	copy(s1[0:4], []byte{0, 0, 0, 0}) // 서버 타임스탬프, 예제에서는 0으로 설정
	copy(s1[4:], c1[4:])              // 클라이언트의 무작위 데이터를 복사

	if _, err := conn.Write(s1); err != nil {
		return fmt.Errorf("failed to write S1: %w", err)
	}

	// C2 읽기
	c2 := make([]byte, 1536)
	if _, err := conn.Read(c2); err != nil {
		return fmt.Errorf("failed to read C2: %w", err)
	}

	// S2: 클라이언트의 C1을 그대로 보냄
	if _, err := conn.Write(c1); err != nil {
		return fmt.Errorf("failed to write S2: %w", err)
	}

	return nil
}

func readRTMPMessages(conn net.Conn) {
	buffer := make([]byte, 4096) // 임시 버퍼 크기 설정

	for {
		// 클라이언트로부터 데이터 읽기
		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Client disconnected:", conn.RemoteAddr())
			} else {
				fmt.Println("Error reading data:", err)
			}
			return
		}

		// 받은 데이터 출력
		fmt.Printf("Received %d bytes of data\n", n)
	}
}
