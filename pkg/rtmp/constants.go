package rtmp

// RTMP 메시지 타입 상수
const (
	MsgTypeSetChunkSize     = 1
	MsgTypeAbort            = 2
	MsgTypeAcknowledgement  = 3
	MsgTypeUserControl      = 4
	MsgTypeWindowAckSize    = 5
	MsgTypeSetPeerBW        = 6
	MsgTypeAudio            = 8
	MsgTypeVideo            = 9
	MsgTypeAMF3Data         = 15
	MsgTypeAMF3SharedObject = 16
	MsgTypeAMF3Command      = 17
	MsgTypeAMF0Data         = 18
	MsgTypeAMF0SharedObject = 19
	MsgTypeAMF0Command      = 20
)

// 청크 스트림 ID 상수
const (
	ChunkStreamProtocol = 2 // 프로토콜 제어 메시지
	ChunkStreamCommand  = 3 // 명령어 메시지 (connect, publish, play 등)
	ChunkStreamAudio    = 4 // 오디오 데이터
	ChunkStreamVideo    = 5 // 비디오 데이터
	ChunkStreamScript   = 6 // 스크립트 데이터 (onMetaData 등)
)

// RTMP 버전
const (
	RTMPVersion = 0x03
)

// 핸드셰이크 상수
const (
	HandshakeSize = 1536
)

// 기본 청크 크기
const (
	DefaultChunkSize = 128
	MaxChunkSize     = 65536
)

// 확장 타임스탬프 임계값
const (
	ExtendedTimestampThreshold = 0xFFFFFF
)

// Fmt 타입 상수 (청크 헤더 형식)
const (
	FmtType0 = 0 // 11바이트 - 전체 메시지 헤더
	FmtType1 = 1 // 7바이트 - 스트림 ID 제외
	FmtType2 = 2 // 3바이트 - 타임스탬프만
	FmtType3 = 3 // 0바이트 - 헤더 없음
)
