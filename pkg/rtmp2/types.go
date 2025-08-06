package rtmp2

import (
	"sol/pkg/media"
)

// RTMP2 전용 타입 정의

// RTMPStreamConfig RTMP 스트림 설정 구조체
type RTMPStreamConfig struct {
	GopCacheSize        int
	MaxPlayersPerStream int
}

// RTMPSessionType 세션 타입 정의
type RTMPSessionType string

const (
	RTMPSessionTypePublisher RTMPSessionType = "publisher"
	RTMPSessionTypePlayer    RTMPSessionType = "player"
)

// RTMPSessionInfo RTMP 세션 정보
type RTMPSessionInfo struct {
	SessionID  string
	StreamName string
	AppName    string
	StreamID   uint32
	Type       RTMPSessionType
}

// RTMPFrameType RTMP 프레임 타입 정의
type RTMPFrameType string

const (
	RTMPFrameTypeKeyFrame              RTMPFrameType = "key frame"
	RTMPFrameTypeInterFrame           RTMPFrameType = "inter frame"
	RTMPFrameTypeDisposableInterFrame RTMPFrameType = "disposable inter frame"
	RTMPFrameTypeGeneratedKeyFrame    RTMPFrameType = "generated key frame"
	RTMPFrameTypeVideoInfoFrame       RTMPFrameType = "video info/command frame"
	RTMPFrameTypeAVCSequenceHeader    RTMPFrameType = "AVC sequence header"
	RTMPFrameTypeAVCNALU              RTMPFrameType = "AVC NALU"
	RTMPFrameTypeAVCEndOfSequence     RTMPFrameType = "AVC end of sequence"
	RTMPFrameTypeAACSequenceHeader    RTMPFrameType = "AAC sequence header"
	RTMPFrameTypeAACRaw               RTMPFrameType = "AAC raw"
)

// convertRTMPFrameToMediaFrame RTMP 프레임을 pkg/media Frame으로 변환
func convertRTMPFrameToMediaFrame(frameType RTMPFrameType, timestamp uint32, data [][]byte, isVideo bool) media.Frame {
	var mediaType media.Type
	if isVideo {
		mediaType = media.TypeVideo
	} else {
		mediaType = media.TypeAudio
	}

	return media.Frame{
		Type:      mediaType,
		FrameType: string(frameType),
		Timestamp: timestamp,
		Data:      data,
	}
}

