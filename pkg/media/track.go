package media

// TimeScale 상수들 (1초당 tick 수)
const (
	// 프로토콜별 표준
	TimeScaleRTMP      = uint32(1000)  // 1ms 단위 (RTMP 표준)
	TimeScaleRTP_Video = uint32(90000) // 90kHz (RTP 비디오 표준)
	TimeScaleRTP_Audio = uint32(48000) // 48kHz (RTP 오디오 표준)
	TimeScaleSRT       = uint32(90000) // 90kHz (SRT 표준)
)

// Track 스트림 내의 개별 미디어 트랙 (버퍼 포함)
type Track struct {
	Index     int          // 트랙 인덱스 (0, 1, 2...)
	Codec     Codec        // 트랙의 코덱
	TimeScale uint32       // 트랙의 시간 스케일 (1초당 tick 수)
	Buffer    *TrackBuffer // 트랙별 개별 버퍼
}

// NewTrack 새로운 트랙 생성 (버퍼 포함)
func NewTrack(index int, codec Codec, timeScale uint32) *Track {
	return &Track{
		Index:     index,
		Codec:     codec,
		TimeScale: timeScale,
		Buffer:    NewTrackBuffer(),
	}
}
