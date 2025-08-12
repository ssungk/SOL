package codec

import "errors"

// 코덱 패키지용 에러들
var (
	ErrInvalidNALU = errors.New("invalid NALU")
	ErrInvalidADTS = errors.New("invalid ADTS header")
)