// Package mpegts는 MPEG-TS (Transport Stream) 형식의 파싱과 생성을 제공합니다.
// SRT, UDP MPEGTS, HLS 등 다양한 스트리밍 프로토콜에서 재사용 가능한 라이브러리입니다.
package mpegts

import (
	"context"
	"errors"
	"fmt"
	"sol/pkg/media"
)

// 공통 에러 정의
var (
	ErrInvalidPacketSize     = errors.New("invalid TS packet size")
	ErrInvalidSyncByte      = errors.New("invalid sync byte")
	ErrInvalidTableID       = errors.New("invalid table ID")
	ErrInvalidSectionLength = errors.New("invalid section length")
	ErrInvalidCRC           = errors.New("invalid CRC32")
	ErrBufferTooSmall       = errors.New("buffer too small")
	ErrUnsupportedStream    = errors.New("unsupported stream type")
	ErrInvalidNALU          = errors.New("invalid NALU")
	ErrInvalidADTS          = errors.New("invalid ADTS header")
)

// ParseError 파싱 에러 (상세 정보 포함)
type ParseError struct {
	Err    error  // 원본 에러
	Offset int    // 에러 발생 위치
	Data   []byte // 관련 데이터 (디버깅용)
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at offset %d: %v", e.Offset, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

// StreamInfo 스트림 정보
type StreamInfo struct {
	PID       uint16     // 스트림 PID
	Type      StreamType // 스트림 타입
	CodecData *CodecData // 코덱 설정 데이터
}

// FrameCallback 프레임 처리 콜백
type FrameCallback func(streamPID uint16, frame media.Frame) error

// MetadataCallback 메타데이터 처리 콜백
type MetadataCallback func(streamPID uint16, metadata map[string]string) error

// Demuxer 인터페이스 - MPEGTS 스트림을 미디어 프레임으로 변환
type Demuxer interface {
	// ProcessData 연속된 TS 패킷 데이터를 처리
	ProcessData(data []byte) error
	
	// SetFrameCallback 프레임 처리 콜백 설정
	SetFrameCallback(callback FrameCallback)
	
	// SetMetadataCallback 메타데이터 처리 콜백 설정
	SetMetadataCallback(callback MetadataCallback)
	
	// GetStreamInfo 스트림 정보 반환
	GetStreamInfo() map[uint16]StreamInfo
	
	// Close 리소스 정리
	Close() error
}

// Muxer 인터페이스 - 미디어 프레임을 MPEGTS 스트림으로 변환
type Muxer interface {
	// AddStream 스트림 추가
	AddStream(streamType StreamType, codecData *CodecData) (uint16, error) // PID 반환
	
	// WriteFrame 프레임 쓰기
	WriteFrame(streamPID uint16, frame media.Frame) ([]byte, error) // TS 패킷들 반환
	
	// WriteMetadata 메타데이터 쓰기
	WriteMetadata(streamPID uint16, metadata map[string]string) ([]byte, error)
	
	// GetPAT PAT 테이블 생성
	GetPAT() ([]byte, error)
	
	// GetPMT PMT 테이블 생성  
	GetPMT(programNumber uint16) ([]byte, error)
	
	// Close 리소스 정리
	Close() error
}

// DemuxerConfig 디먹서 설정
type DemuxerConfig struct {
	// BufferSize 내부 버퍼 크기
	BufferSize int
	
	// EnableDebugLog 디버그 로그 활성화
	EnableDebugLog bool
	
	// MaxPIDCount 최대 PID 개수
	MaxPIDCount int
	
	// PacketLossThreshold 패킷 손실 임계값
	PacketLossThreshold int
}

// MuxerConfig 먹서 설정
type MuxerConfig struct {
	// ProgramNumber 프로그램 번호
	ProgramNumber uint16
	
	// TransportStreamID 전송 스트림 ID
	TransportStreamID uint16
	
	// PMT_PID PMT PID (기본: 0x100)
	PMT_PID uint16
	
	// PCR_PID PCR PID (기본: 첫 번째 비디오 스트림)
	PCR_PID uint16
	
	// EnableDebugLog 디버그 로그 활성화
	EnableDebugLog bool
}

// NewDemuxer 새 디먹서 생성
func NewDemuxer(ctx context.Context, config DemuxerConfig) Demuxer {
	return newDemuxer(ctx, config)
}

// NewMuxer 새 먹서 생성
func NewMuxer(ctx context.Context, config MuxerConfig) Muxer {
	return newMuxer(ctx, config)
}

// ParseTSPacket TS 패킷 파싱
func ParseTSPacket(data []byte) (*TSPacket, error) {
	return parseTSPacket(data)
}

// CreateTSPacket TS 패킷 생성
func CreateTSPacket(header TSPacketHeader, adaptationField *AdaptationField, payload []byte) ([]byte, error) {
	return createTSPacket(header, adaptationField, payload)
}

// ParsePAT PAT 테이블 파싱
func ParsePAT(data []byte) (*PAT, error) {
	return parsePAT(data)
}

// CreatePAT PAT 테이블 생성
func CreatePAT(transportStreamID uint16, programs []PATEntry) ([]byte, error) {
	return createPAT(transportStreamID, programs)
}

// ParsePMT PMT 테이블 파싱
func ParsePMT(data []byte) (*PMT, error) {
	return parsePMT(data)
}

// CreatePMT PMT 테이블 생성
func CreatePMT(programNumber uint16, pcrPID uint16, streams []PMTStreamInfo) ([]byte, error) {
	return createPMT(programNumber, pcrPID, streams)
}

// ParsePESPacket PES 패킷 파싱
func ParsePESPacket(data []byte) (*PESPacket, error) {
	return parsePESPacket(data)
}

// CreatePESPacket PES 패킷 생성
func CreatePESPacket(streamID uint8, pts, dts uint64, data []byte) ([]byte, error) {
	return createPESPacket(streamID, pts, dts, data)
}

// ConvertToMediaFrame MPEGTS에서 추출한 데이터를 media.Frame으로 변환
func ConvertToMediaFrame(streamType StreamType, nalus []NALU, timestamp uint32) media.Frame {
	var frameType media.Type
	var frameSubType media.FrameSubType
	var codecType media.CodecType
	var formatType media.FormatType
	var data [][]byte
	
	switch streamType {
	case StreamTypeH264:
		frameType = media.TypeVideo
		codecType = media.CodecH264
		formatType = media.FormatStartCode // MPEGTS는 StartCode 포맷 사용
		
		// 키프레임 또는 설정 프레임 판별
		isKeyFrame := false
		isConfigFrame := false
		
		for _, nalu := range nalus {
			if nalu.IsKey {
				isKeyFrame = true
			}
			if nalu.IsConfig {
				isConfigFrame = true
			}
			
			// NALU 데이터를 [][]byte로 변환 (제로카피)
			data = append(data, nalu.Data)
		}
		
		if isConfigFrame {
			frameSubType = media.VideoSequenceHeader
		} else if isKeyFrame {
			frameSubType = media.VideoKeyFrame
		} else {
			frameSubType = media.VideoInterFrame
		}
		
	case StreamTypeH265:
		frameType = media.TypeVideo
		codecType = media.CodecH265
		formatType = media.FormatStartCode // MPEGTS는 StartCode 포맷 사용
		
		// 키프레임 또는 설정 프레임 판별
		isKeyFrame := false
		isConfigFrame := false
		
		for _, nalu := range nalus {
			if nalu.IsKey {
				isKeyFrame = true
			}
			if nalu.IsConfig {
				isConfigFrame = true
			}
			
			// NALU 데이터를 [][]byte로 변환 (제로카피)
			data = append(data, nalu.Data)
		}
		
		if isConfigFrame {
			frameSubType = media.VideoSequenceHeader
		} else if isKeyFrame {
			frameSubType = media.VideoKeyFrame
		} else {
			frameSubType = media.VideoInterFrame
		}
		
	case StreamTypeADTS, StreamTypeAAC:
		frameType = media.TypeAudio
		codecType = media.CodecAAC
		formatType = media.FormatADTS // AAC는 ADTS 포맷 사용
		
		// AAC 프레임의 경우 설정 정보가 포함되어 있는지 확인
		if len(nalus) > 0 && nalus[0].IsConfig {
			frameSubType = media.AudioSequenceHeader
		} else {
			frameSubType = media.AudioRawData
		}
		
		// 오디오 데이터를 [][]byte로 변환
		for _, nalu := range nalus {
			data = append(data, nalu.Data)
		}
		
	default:
		// 알 수 없는 스트림 타입의 경우 메타데이터로 처리
		frameType = media.TypeMetadata
		frameSubType = media.FrameSubType(0)
		codecType = media.CodecUnknown
		formatType = media.FormatRaw
		
		for _, nalu := range nalus {
			data = append(data, nalu.Data)
		}
	}
	
	return media.Frame{
		Type:       frameType,
		SubType:    frameSubType,
		CodecType:  codecType,
		FormatType: formatType,
		Timestamp:  timestamp,
		Data:       data,
	}
}

// ExtractTimestamp PTS/DTS에서 밀리초 단위 타임스탬프 추출
func ExtractTimestamp(pts uint64) uint32 {
	// PTS는 90kHz 클록 기준 (90000 = 1초)
	// 밀리초로 변환: pts / 90
	return uint32(pts / 90)
}