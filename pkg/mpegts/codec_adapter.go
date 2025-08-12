package mpegts

import (
	"sol/pkg/mpegts/codec"
)

// 코덱 패키지와의 타입 변환 함수들

// ConvertFromCodecNALUs codec.NALU 배열을 mpegts.NALU 배열로 변환
func ConvertFromCodecNALUs(codecNALUs []codec.NALU) []NALU {
	var nalus []NALU
	for _, codecNALU := range codecNALUs {
		nalu := NALU{
			Type:     NALUType(codecNALU.Type),
			Data:     codecNALU.Data,
			IsConfig: codecNALU.IsConfig,
			IsKey:    codecNALU.IsKey,
		}
		nalus = append(nalus, nalu)
	}
	return nalus
}

// ConvertToCodecNALUs mpegts.NALU 배열을 codec.NALU 배열로 변환
func ConvertToCodecNALUs(nalus []NALU) []codec.NALU {
	var codecNALUs []codec.NALU
	for _, nalu := range nalus {
		codecNALU := codec.NALU{
			Type:     codec.NALUType(nalu.Type),
			Data:     nalu.Data,
			IsConfig: nalu.IsConfig,
			IsKey:    nalu.IsKey,
		}
		codecNALUs = append(codecNALUs, codecNALU)
	}
	return codecNALUs
}

// ConvertFromCodecData codec.CodecData를 mpegts.CodecData로 변환
func ConvertFromCodecData(codecData *codec.CodecData) *CodecData {
	if codecData == nil {
		return nil
	}
	
	return &CodecData{
		Type:       StreamType(codecData.Type),
		SPS:        codecData.SPS,
		PPS:        codecData.PPS,
		VPS:        codecData.VPS,
		ASC:        codecData.ASC,
		Profile:    codecData.Profile,
		Level:      codecData.Level,
		Width:      codecData.Width,
		Height:     codecData.Height,
		FPS:        codecData.FPS,
		Channels:   codecData.Channels,
		SampleRate: codecData.SampleRate,
	}
}

// ConvertToCodecData mpegts.CodecData를 codec.CodecData로 변환
func ConvertToCodecData(codecData *CodecData) *codec.CodecData {
	if codecData == nil {
		return nil
	}
	
	return &codec.CodecData{
		Type:       codec.StreamType(codecData.Type),
		SPS:        codecData.SPS,
		PPS:        codecData.PPS,
		VPS:        codecData.VPS,
		ASC:        codecData.ASC,
		Profile:    codecData.Profile,
		Level:      codecData.Level,
		Width:      codecData.Width,
		Height:     codecData.Height,
		FPS:        codecData.FPS,
		Channels:   codecData.Channels,
		SampleRate: codecData.SampleRate,
	}
}