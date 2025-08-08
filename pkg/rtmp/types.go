package rtmp

import (
	"sol/pkg/media"
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
	RTMPFrameTypeAVCEndOfSequence     RTMPFrameType = "AVC end of sequence"
	RTMPFrameTypeAACSequenceHeader    RTMPFrameType = "AAC sequence header"
	RTMPFrameTypeAACRaw               RTMPFrameType = "AAC raw"
)

// RTMPFrameTypeToFrameSubType RTMP 프레임 타입을 media.FrameSubType으로 변환
func RTMPFrameTypeToFrameSubType(rtmpType RTMPFrameType) media.FrameSubType {
	switch rtmpType {
	case RTMPFrameTypeKeyFrame:
		return media.VideoKeyFrame
	case RTMPFrameTypeInterFrame:
		return media.VideoInterFrame
	case RTMPFrameTypeDisposableInterFrame:
		return media.VideoDisposableInterFrame
	case RTMPFrameTypeGeneratedKeyFrame:
		return media.VideoGeneratedKeyFrame
	case RTMPFrameTypeVideoInfoFrame:
		return media.VideoInfoFrame
	case RTMPFrameTypeAVCSequenceHeader:
		return media.VideoSequenceHeader
	case RTMPFrameTypeAVCEndOfSequence:
		return media.VideoEndOfSequence
	case RTMPFrameTypeAACSequenceHeader:
		return media.AudioSequenceHeader
	case RTMPFrameTypeAACRaw:
		return media.AudioRawData
	default:
		// 기본값 반환
		return media.VideoInterFrame
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
	managedFrame := media.NewManagedFrame(mediaType, RTMPFrameTypeToFrameSubType(frameType), timestamp, poolManager)

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
