package media

// Track 스트림 내의 개별 미디어 트랙 (버퍼 포함)
type Track struct {
	Index  int          // 트랙 인덱스 (0, 1, 2...)
	Codec  Codec        // 트랙의 코덱
	Buffer *TrackBuffer // 트랙별 개별 버퍼
}

// NewTrack 새로운 트랙 생성 (버퍼 포함)
func NewTrack(index int, codec Codec) *Track {
	return &Track{
		Index:  index,
		Codec:  codec,
		Buffer: NewTrackBuffer(),
	}
}