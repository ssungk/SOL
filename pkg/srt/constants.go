package srt

// SRT 프로토콜 상수 정의
const (
	// SRT 기본 포트
	DefaultPort = 9999
	
	// SRT 패킷 크기
	MTU              = 1500
	UDTHeaderSize    = 16
	SRTHeaderSize    = 16
	MaxPayloadSize   = MTU - UDTHeaderSize - SRTHeaderSize
	
	// 버퍼 크기
	DefaultLatency   = 120 // 기본 지연시간 (ms)
	MinLatency      = 20   // 최소 지연시간 (ms)
	MaxLatency      = 8000 // 최대 지연시간 (ms)
	
	// 타임아웃
	HandshakeTimeout = 3000 // 핸드셰이크 타임아웃 (ms)
	KeepAliveTimeout = 1000 // 킵얼라이브 타임아웃 (ms)
	
	// 재전송 관련
	MaxRetries       = 3
	RetryInterval    = 250 // ms
	
	// 연결 상태
	StateInit        = 1
	StateConnecting  = 2 
	StateConnected   = 3
	StateClosing     = 4
	StateClosed      = 5
	
	// SRT 버전
	SRTVersion       = 0x010401 // SRT v1.4.1
	
	// SRT 플래그
	SRTFlagTSBPD     = 0x00000001 // Timestamp-based Packet Delivery
	SRTFlagNAKREPORT = 0x00000002 // NAK Reports
	SRTFlagREXMITFLG = 0x00000004 // Retransmission Flag
	
	// 암호화
	EncryptionNone   = 0
	EncryptionAES128 = 1
	EncryptionAES192 = 2
	EncryptionAES256 = 3
)