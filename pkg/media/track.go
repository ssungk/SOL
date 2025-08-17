package media

// Track 스트림 내의 개별 미디어 트랙
type Track struct {
	Index int   // 트랙 인덱스 (0, 1, 2...)
	Codec Codec // 트랙의 코덱
}

// NewTrack 새로운 트랙 생성
func NewTrack(index int, codec Codec) *Track {
	return &Track{
		Index: index,
		Codec: codec,
	}
}