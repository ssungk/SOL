package utils

import "fmt"

// TypeName 런타임 타입의 이름을 문자열로 반환하는 헬퍼 함수
func TypeName(v any) string {
	return fmt.Sprintf("%T", v)
}