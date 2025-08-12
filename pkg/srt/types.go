package srt

import (
	"net"
	"time"
)

// SRTConfig SRT 서버 설정
type SRTConfig struct {
	Port         int           // 서버 포트
	Latency      int           // 지연시간 (ms)
	MaxBandwidth int64         // 최대 대역폭 (bps)
	Encryption   int           // 암호화 레벨
	Passphrase   string        // 암호화 패스워드
	StreamID     string        // 기본 스트림 ID 패턴
	Timeout      time.Duration // 연결 타임아웃
}

// NewSRTConfig 기본 SRT 설정 생성
func NewSRTConfig(port int, latency int) SRTConfig {
	return SRTConfig{
		Port:         port,
		Latency:      latency,
		MaxBandwidth: 0, // 0 = 무제한
		Encryption:   EncryptionNone,
		Passphrase:   "",
		StreamID:     "",
		Timeout:      5 * time.Second,
	}
}

// UDTPacket UDT 기반 패킷 구조 (SRT는 UDT 기반)
type UDTPacket struct {
	Header  UDTHeader
	Payload []byte
}

// UDTHeader 표준 UDT 헤더 구조 (16바이트)
type UDTHeader struct {
	// 첫 4바이트: Packet Sequence Number OR Control Type
	SeqNum uint32 // 데이터 패킷의 경우 시퀀스 번호 (MSB=0)
	                // 제어 패킷의 경우 Control Type (MSB=1)
	
	// 다음 4바이트: Message Number OR Additional Info
	MessageNumber uint32 // 데이터: 메시지 번호, 제어: 추가 정보
	
	// 다음 4바이트: Timestamp (마이크로초)
	Timestamp uint32
	
	// 마지막 4바이트: Destination Socket ID
	DestSocketID uint32
}

// SRTExtensionHeader SRT 확장 헤더 (핸드셰이크 시 사용)
type SRTExtensionHeader struct {
	Version       uint32 // SRT 버전
	EncryptFlags  uint32 // 암호화 플래그  
	Extension     uint32 // 확장 필드
	// StreamID는 별도 확장으로 처리
}

// SRTHandshakePacket SRT 핸드셰이크 패킷
type SRTHandshakePacket struct {
	UDTVersion        uint32    // UDT 버전 (항상 4)
	SocketType        uint32    // 소켓 타입 (1: 스트림)
	InitPacketSeq     uint32    // 초기 패킷 시퀀스 번호
	MaxPacketSize     uint32    // 최대 패킷 크기
	MaxFlowWinSize    uint32    // 최대 플로우 윈도우 크기
	HandshakeType     uint32    // 핸드셰이크 타입
	SocketID          uint32    // 소켓 ID
	SynCookie         uint32    // SYN 쿠키
	PeerIPAddress     [16]byte  // 피어 IP 주소
	
	// SRT 확장 (핸드셰이크 완료 후)
	SRTFlags          uint32    // SRT 플래그
	RecvTSBPDDelay    uint16    // 수신 TSBPD 지연 (ms)  
	SendTSBPDDelay    uint16    // 송신 TSBPD 지연 (ms)
}

// ControlPacketType 제어 패킷 타입
type ControlPacketType uint32

const (
	ControlHandshake ControlPacketType = 0x0
	ControlKeepAlive ControlPacketType = 0x1  
	ControlAck       ControlPacketType = 0x2
	ControlNak       ControlPacketType = 0x3
	ControlCongestion ControlPacketType = 0x4
	ControlShutdown   ControlPacketType = 0x5
	ControlAckAck     ControlPacketType = 0x6
	ControlDropReq    ControlPacketType = 0x7
	ControlPeerError  ControlPacketType = 0x8
)

// 패킷 타입 확인 함수들
func (h *UDTHeader) IsControlPacket() bool {
	return (h.SeqNum & 0x80000000) != 0
}

func (h *UDTHeader) GetControlType() ControlPacketType {
	return ControlPacketType(h.SeqNum & 0x7FFFFFFF)
}

func (h *UDTHeader) GetSequenceNumber() uint32 {
	return h.SeqNum & 0x7FFFFFFF
}

// ConnectionInfo SRT 연결 정보
type ConnectionInfo struct {
	RemoteAddr     net.Addr
	SocketID       uint32
	PeerSocketID   uint32
	StreamID       string
	State          int
	ConnectedAt    time.Time
	LastActivity   time.Time
	BytesSent      uint64
	BytesReceived  uint64
	PacketsSent    uint64
	PacketsReceived uint64
	PacketsLost    uint64
	PacketsRetrans uint64
}

// SRTStats SRT 통계 정보
type SRTStats struct {
	ConnectionInfo
	RTT            time.Duration // 왕복 시간
	Bandwidth      int64         // 현재 대역폭
	Latency        time.Duration // 현재 지연시간
	LossRate       float64       // 패킷 손실률
	Jitter         time.Duration // 지터
}

// HandshakeRequest 핸드셰이크 요청
type HandshakeRequest struct {
	Version        uint32
	EncryptionType uint32
	Extension      uint32
	InitialSeq     uint32
	MaxTransUnit   uint32
	MaxFlowWindow  uint32
	HandshakeType  uint32
	SocketID       uint32
	SynCookie      uint32
	PeerIP         [16]byte
	Extensions     []HandshakeExtension
}

// HandshakeExtension 핸드셰이크 확장
type HandshakeExtension struct {
	Type   uint16
	Length uint16
	Data   []byte
}

// SRTEvent SRT 이벤트 타입들
type SRTEvent interface {
	EventType() string
}

// SRTConnectionEvent 연결 이벤트
type SRTConnectionEvent struct {
	SocketID   uint32
	RemoteAddr net.Addr
	StreamID   string
}

func (e SRTConnectionEvent) EventType() string {
	return "srt_connection"
}

// SRTDisconnectionEvent 연결 해제 이벤트  
type SRTDisconnectionEvent struct {
	SocketID   uint32
	RemoteAddr net.Addr
	StreamID   string
	Reason     string
}

func (e SRTDisconnectionEvent) EventType() string {
	return "srt_disconnection"
}

// SRTDataEvent 데이터 수신 이벤트
type SRTDataEvent struct {
	SocketID uint32
	StreamID string
	Data     []byte
	Size     int
}

func (e SRTDataEvent) EventType() string {
	return "srt_data"
}