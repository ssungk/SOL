package rtmp

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

// RTMPFrameType RTMP 프레임 타입 정의
type RTMPFrameType string

const (
	RTMPFrameTypeKeyFrame             RTMPFrameType = "key frame"
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

// convertRTMPFrameToManagedFrame RTMP 프레임을 ManagedFrame으로 변환 (pool 추적)
func convertRTMPFrameToManagedFrame(frameType RTMPFrameType, timestamp uint32, data [][]byte, isVideo bool, poolManager *media.PoolManager) *media.ManagedFrame {
	var mediaType media.Type
	if isVideo {
		mediaType = media.TypeVideo
	} else {
		mediaType = media.TypeAudio
	}

	// ManagedFrame 생성
	managedFrame := media.NewManagedFrame(mediaType, string(frameType), timestamp, poolManager)
	
	// 각 데이터 청크를 추가
	for _, chunk := range data {
		if len(chunk) > 0 {
			// Pool에서 할당된 버퍼인지 확인하여 적절히 추가
			// PoolManager에 등록되어 있으면 pooled chunk로, 아니면 regular chunk로 추가
			managedFrame.AddRegularChunk(chunk)
		}
	}
	
	return managedFrame
}
