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
