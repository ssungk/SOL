package media

// NodeType represents different media node types (protocols and processors)
type NodeType uint8

const (
	NodeTypeRTMP NodeType = iota + 1
	NodeTypeRTSP
	NodeTypeSRT
	NodeTypeWebRTC
	NodeTypeHLS
	NodeTypeDASH
	NodeTypeFile
	NodeTypeTranscoder
	NodeTypeAnalyzer
)

// 공통 상수들
const (
	// DefaultChannelBufferSize 기본 채널 버퍼 크기 - 모든 프로토콜에서 공통 사용
	DefaultChannelBufferSize = 20
)

// String returns string representation of NodeType for debugging
func (nt NodeType) String() string {
	switch nt {
	case NodeTypeRTMP:
		return "rtmp"
	case NodeTypeRTSP:
		return "rtsp"
	case NodeTypeSRT:
		return "srt"
	case NodeTypeWebRTC:
		return "webrtc"
	case NodeTypeHLS:
		return "hls"
	case NodeTypeDASH:
		return "dash"
	case NodeTypeFile:
		return "file"
	case NodeTypeTranscoder:
		return "transcoder"
	case NodeTypeAnalyzer:
		return "analyzer"
	default:
		return "unknown"
	}
}
