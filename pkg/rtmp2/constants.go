package rtmp2

// RTMP 메시지 타입 상수
const (
	MSG_TYPE_SET_CHUNK_SIZE     = 1
	MSG_TYPE_ABORT              = 2
	MSG_TYPE_ACKNOWLEDGEMENT    = 3
	MSG_TYPE_USER_CONTROL       = 4
	MSG_TYPE_WINDOW_ACK_SIZE    = 5
	MSG_TYPE_SET_PEER_BW        = 6
	MSG_TYPE_AUDIO              = 8
	MSG_TYPE_VIDEO              = 9
	MSG_TYPE_AMF3_DATA          = 15
	MSG_TYPE_AMF3_SHARED_OBJECT = 16
	MSG_TYPE_AMF3_COMMAND       = 17
	MSG_TYPE_AMF0_DATA          = 18
	MSG_TYPE_AMF0_SHARED_OBJECT = 19
	MSG_TYPE_AMF0_COMMAND       = 20
)

// 청크 스트림 ID 상수
const (
	CHUNK_STREAM_PROTOCOL = 2 // 프로토콜 제어 메시지 (Set Chunk Size 등)
	CHUNK_STREAM_COMMAND  = 3 // 명령어 메시지 (connect, publish, play 등)
	CHUNK_STREAM_AUDIO    = 4 // 오디오 데이터
	CHUNK_STREAM_VIDEO    = 5 // 비디오 데이터
	CHUNK_STREAM_SCRIPT   = 6 // 스크립트 데이터 (onMetaData 등)
)

// RTMP 버전
const (
	RTMP_VERSION = 0x03
)

// 핸드셰이크 상수
const (
	HANDSHAKE_SIZE = 1536
)

// 기본 청크 크기
const (
	DEFAULT_CHUNK_SIZE = 128
	MAX_CHUNK_SIZE     = 65536
)

// 확장 타임스탬프 임계값
const (
	EXTENDED_TIMESTAMP_THRESHOLD = 0xFFFFFF
)

// Fmt 타입 상수 (청크 헤더 형식)
const (
	FMT_TYPE_0 = 0 // 11바이트 - 전체 메시지 헤더
	FMT_TYPE_1 = 1 // 7바이트 - 스트림 ID 제외
	FMT_TYPE_2 = 2 // 3바이트 - 타임스탬프만
	FMT_TYPE_3 = 3 // 0바이트 - 헤더 없음
)