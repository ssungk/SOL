package sol

import (
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

// initLogger는 애플리케이션의 기본 slog 로거를 설정합니다.
func InitLogger(config *Config) {
	// 프로젝트의 루트 경로를 정의합니다.
	// 일반적으로 main 함수가 있는 디렉토리나, go.mod 파일이 있는 디렉토리를 기준으로 합니다.
	// 현재 실행 파일의 디렉토리를 기준으로 프로젝트 루트를 찾습니다.
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := getProjectRoot(filename) // 프로젝트 루트 경로를 계산하는 헬퍼 함수

	// ReplaceAttr 함수를 정의합니다.
	// 이 함수는 각 로그 속성을 처리할 때 호출됩니다.
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		// slog.SourceKey ("source") 속성을 찾습니다.
		if a.Key == slog.SourceKey {
			source, ok := a.Value.Any().(*slog.Source)
			if !ok {
				return a // slog.Source 타입이 아니면 원래 속성 반환
			}

			// 파일 경로를 프로젝트 루트에 대한 상대 경로로 변환합니다.
			if projectRoot != "" && strings.HasPrefix(source.File, projectRoot) {
				// projectRoot 뒤의 슬래시까지 포함하여 자릅니다.
				source.File = source.File[len(projectRoot)+1:]
			} else {
				// 프로젝트 루트를 벗어나는 파일 (예: Go 표준 라이브러리)의 경우
				// 파일 경로를 basename만 남기거나 다른 처리를 할 수 있습니다.
				// 여기서는 전체 경로를 그대로 둡니다.
				// source.File = filepath.Base(source.File) // 파일 이름만 남기려면
			}
			return slog.Any(a.Key, source) // 수정된 source로 속성 업데이트
		}
		return a // 다른 속성은 변경 없이 반환
	}

	// tint.NewHandler를 사용하여 컬러 출력 및 slog.HandlerOptions 설정을 합니다.
	handler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      config.GetSlogLevel(), // 설정에서 로그 레벨 가져오기
		AddSource:  true,            // 소스 코드 정보 포함 (ReplaceAttr와 함께 사용)
		NoColor:    false,           // 컬러 출력 활성화
		TimeFormat: time.RFC3339,    // 시간 포맷
		// ReplaceAttr를 여기에 전달합니다. tint 핸들러는 내부적으로 이 옵션을 slog.HandlerOptions로 전달합니다.
		ReplaceAttr: replaceAttr,
	})

	// 생성된 핸들러로 slog.Logger 인스턴스를 만들고 기본 로거로 설정합니다.
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// getProjectRoot는 주어진 파일 경로에서 프로젝트 루트 경로를 추론하는 헬퍼 함수입니다.
// 실제 프로젝트에서는 go.mod 파일을 찾거나, 고정된 경로를 사용하기도 합니다.
// 여기서는 간략화를 위해 main.go 파일이 있는 디렉토리를 루트로 가정합니다.
func getProjectRoot(filepath string) string {
	// runtime.Caller(0)은 호출 지점의 파일 경로를 반환합니다.
	// 이 경로에서 os.PathSeparator를 찾아 그 이전까지를 루트로 간주합니다.
	dir := filepath // 예: /home/user/myproject/main.go
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == os.PathSeparator {
			return dir[:i] // /home/user/myproject 반환
		}
	}
	return ""
}
