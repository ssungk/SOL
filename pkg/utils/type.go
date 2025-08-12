package utils

import (
	"encoding/binary"
	"fmt"
)

// TypeName 런타임 타입의 이름을 문자열로 반환하는 헬퍼 함수
func TypeName(v any) string {
	return fmt.Sprintf("%T", v)
}

// PutUint32BE uint32 값을 big-endian 바이트로 변환
func PutUint32BE(buf []byte, val uint32) {
	binary.BigEndian.PutUint32(buf, val)
}

// GetUint32BE big-endian 바이트에서 uint32 값 추출
func GetUint32BE(buf []byte) uint32 {
	return binary.BigEndian.Uint32(buf)
}