package media

// MediaNodeType represents different media node types (protocols and processors)
type MediaNodeType uint8

const (
	MediaNodeTypeRTMP MediaNodeType = iota + 1
	MediaNodeTypeRTSP
	MediaNodeTypeSRT
	MediaNodeTypeWebRTC
	MediaNodeTypeHLS
	MediaNodeTypeDASH
	MediaNodeTypeFile
	MediaNodeTypeTranscoder
	MediaNodeTypeAnalyzer
)

// String returns string representation of MediaNodeType for debugging
func (mnt MediaNodeType) String() string {
	switch mnt {
	case MediaNodeTypeRTMP:
		return "rtmp"
	case MediaNodeTypeRTSP:
		return "rtsp"
	case MediaNodeTypeSRT:
		return "srt"
	case MediaNodeTypeWebRTC:
		return "webrtc"
	case MediaNodeTypeHLS:
		return "hls"
	case MediaNodeTypeDASH:
		return "dash"
	case MediaNodeTypeFile:
		return "file"
	case MediaNodeTypeTranscoder:
		return "transcoder"
	case MediaNodeTypeAnalyzer:
		return "analyzer"
	default:
		return "unknown"
	}
}
