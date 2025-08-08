package utils

import (
	"io"
	"log/slog"
)

// CloseWithLog 리소스를 안전하게 닫고 에러 발생 시 로그를 남기는 범용 유틸리티 함수
func CloseWithLog(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Error("Error closing resource", "resource", c, "err", err)
	}
}