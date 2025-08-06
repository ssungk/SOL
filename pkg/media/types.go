package media

// MediaType represents different media processing types
type MediaType string

const (
	MediaTypeRTMP       MediaType = "rtmp"
	MediaTypeRTSP       MediaType = "rtsp"
	MediaTypeSRT        MediaType = "srt"
	MediaTypeWebRTC     MediaType = "webrtc"
	MediaTypeHLS        MediaType = "hls"
	MediaTypeDASH       MediaType = "dash"
	MediaTypeFile       MediaType = "file"
	MediaTypeTranscoder MediaType = "transcoder"
	MediaTypeAnalyzer   MediaType = "analyzer"
)