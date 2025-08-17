# CLAUDE.md

이 파일은 Claude Code (claude.ai/code)가 이 저장소에서 작업할 때 필요한 가이드를 제공합니다.

## Claude Code 기본 설정

- **언어**: 항상 한국어로 응답해주세요
- **응답 스타일**: 간결하고 직접적으로 답변
- **기술 용어**: 한국어 우선, 필요시 영어 병기

## 코딩 스타일 가이드

### 주석 작성 규칙
- **기본 언어는 한글**: 함수, 구조체, 패키지 설명 등 주요 주석은 한글로 작성
- **간단한 주석은 영어 허용**: 필드 설명, 간단한 로직 설명 등은 영어 사용 가능
- **패키지 주석**: 패키지의 목적과 사용법을 한글로 명확히 설명
- **함수 주석**: 함수의 역할, 매개변수, 반환값을 한글로 설명
- **구조체 주석**: 구조체의 목적과 사용법을 한글로 설명
- **복잡한 로직 주석**: 알고리즘이나 비즈니스 로직은 한글로 설명
- **로그 메시지는 영어로 작성**: slog, log 등의 로그 메시지는 영어로 유지 (국제 표준)

## 개발 명령어

### 빌드 및 실행
```bash
# 서버 실행
go run cmd/main.go

# 서버 빌드
go build -o sol cmd/main.go

# 테스트 실행
go test ./...

# 상세 출력으로 테스트 실행
go test -v ./...

# 특정 패키지 테스트
go test ./pkg/rtmp/
go test ./pkg/media/
```

### 프로토콜 테스트
```bash
# FFmpeg으로 RTMP 테스트 (발행)
ffmpeg -i input.mp4 -c copy -f flv rtmp://localhost:1935/live/test_stream

# FFplay로 RTMP 테스트 (재생)
ffplay rtmp://localhost:1935/live/test_stream

# FFmpeg으로 RTSP 테스트 (발행)
ffmpeg -re -i input.mp4 -c copy -f rtsp rtsp://localhost:554/live/test_stream

# FFplay로 RTSP 테스트 (재생)
ffplay rtsp://localhost:554/live/test_stream
```

## 아키텍처 개요

SOL은 Go로 구축된 스트리밍 서버로, RTMP와 RTSP 프로토콜을 지원합니다. 관심사의 명확한 분리를 통한 계층화된 접근 방식으로 설계되었습니다.

### 핵심 아키텍처 계층

1. **애플리케이션 계층** (`internal/sol/`)
   - `SOL` 구조체가 전체 애플리케이션을 조율
   - 설정, 로깅, 서버 생명주기 관리
   - 미디어 서버와 API 서버 간 조정

2. **미디어 서버 계층** (`internal/media/`)
   - `MediaServer`가 여러 프로토콜 서버(RTMP, RTSP)를 관리
   - 서버 시작/종료 조정 처리
   - 스트리밍 프로토콜을 위한 통합 설정 제공

3. **API 계층** (`internal/api/`)
   - 외부 제어 및 모니터링을 위한 REST API 서버
   - 스트림 관리 및 통계를 위한 엔드포인트 제공

4. **프로토콜 구현** (`pkg/rtmp/`, `pkg/rtsp/`)
   - 각 프로토콜은 자체 서버, 세션, 메시지 처리를 가짐
   - 프로토콜별 파싱 및 인코딩
   - 모든 프로토콜이 MediaSource/MediaSink 패턴 적용
   - 내부 채널을 사용한 이벤트 드리븐 아키텍처

5. **공통 스트리밍 코어** (`pkg/media/`)
   - 프로토콜 독립적 스트림 관리 (MediaSource/MediaSink 패턴)
   - 미디어 프레임 추상화 및 스마트 버퍼링
   - 발행자-구독자 패턴 구현
   - **프로토콜 간 스트림 공유**: RTMP 발행 → RTSP 재생, RTSP 발행 → RTMP 재생

### 스트림 관리 시스템

스트리밍 시스템은 프로토콜 전반에 걸쳐 통합된 접근 방식을 사용합니다:

- **Stream** (`pkg/media/stream.go`): 미디어 데이터 흐름을 관리하는 프로토콜 독립적 스트림 객체
- **MediaSource 인터페이스**: 스트림 생산자(RTMP/RTSP 세션)를 위한 추상 인터페이스
- **MediaSink 인터페이스**: 스트림 소비자(RTMP/RTSP 세션)를 위한 추상 인터페이스
- **MediaFrame**: 통합 미디어 데이터 구조 (비디오/오디오 프레임 통합)
- **StreamBuffer**: 키프레임 기반 + 시간 기반 스마트 버퍼링, extraData 캐싱

### 이벤트 드리븐 설계

각 프로토콜 서버는 이벤트 드리븐 아키텍처를 사용합니다:
- 비동기 이벤트 처리를 위한 내부 채널
- 서버 이벤트 루프에서 중앙화된 이벤트 처리

### 설정 시스템

설정은 YAML 파일(`configs/default.yaml`)을 통해 관리됩니다:
- 프로토콜별 설정 (포트, 타임아웃)
- 스트림 설정 (GOP 캐시 크기, 최대 플레이어 수)
- 로깅 설정
- 시작 시 명확한 오류 메시지와 함께 검증

## 주요 구현 세부사항

### Zero-Copy 최적화
코드베이스는 메모리 복사를 피하기 위해 파이프라인 전체에서 `[][]byte` 청크를 사용합니다:
- 메시지 파싱이 원래 청크 경계를 보존
- 스트림 버퍼링이 청크 참조를 유지
- 네트워크 쓰기가 연결 없이 청크를 직접 전송

### 세션 관리
- 세션은 세션 ID와 함께 맵을 사용하여 프로토콜 서버에서 직접 관리
- 각 세션은 읽기 및 이벤트 처리를 위한 자체 고루틴을 가짐
- 세션 연결 해제 시 자동 정리

### 스트림 생명주기
1. 발행자 연결 및 발행 시작 → Stream 생성
2. Stream이 키프레임 기반 + 최소 2초 버퍼링 및 메타데이터 캐싱
3. 플레이어 연결 → extraData + 캐시된 프레임 즉시 수신 + 라이브 데이터
4. 발행자 연결 해제 → Stream 정리, 플레이어 연결 해제

### 미디어 데이터 흐름
```
MediaSource.SendFrame() → Stream.SendFrame() → StreamBuffer → MediaSink.SendMediaFrame() → Player Sessions
```

## 일반적인 개발 패턴


### 스트림 처리
- 데이터 주입에는 항상 `Stream.SendMediaFrame()` 및 `Stream.SendMetadata()` 사용
- 스트림 데이터 수신을 위해 `MediaSink` 인터페이스 구현
- 스마트 버퍼링 및 extraData 관리에 `StreamBuffer` 사용

### 설정 변경
- `internal/sol/config.go`의 config 구조체에 새 설정 추가
- 기본값으로 `configs/default.yaml` 업데이트
- config 로딩에 검증 추가

## 테스트 가이드라인

- 핵심 패키지에 단위 테스트 존재: AMF 인코딩/디코딩, RTP 패킷, 메시지 파싱
- 통합 테스트에는 외부 도구(FFmpeg, FFplay) 필요
- 테스트 파일은 소스 코드와 함께 위치 (`*_test.go`)

## 중요 참고사항

- 스트림 관리는 경쟁 상태를 피하기 위해 동기식(스트림 처리에서 고루틴 없음)
- 모든 slog 호출은 일관성을 위해 한 줄로 포맷됨
- **커밋 메시지는 1줄로 작성** (상세 설명 없이 간결하게)
- **커밋에 Co-Authored-By 추가 금지** (Claude Code 관련 정보 포함하지 않기)
- 설정 변경 시 서버 재시작 필요 (핫 리로드 미지원)


## 완료된 주요 기능

### ✅ 최근 완료된 기능들
- [x] **RTMP 패키지 통합**: 기존 pkg/rtmp 삭제, pkg/rtmp2를 pkg/rtmp로 통합
- [x] **RTSP pkg/media 아키텍처 적용**: RTSP 서버가 MediaSource/MediaSink 패턴 사용
- [x] **프로토콜 간 스트림 공유**: RTMP ↔ RTSP 간 실시간 스트림 공유 가능
- [x] **이벤트 시스템 통합**: 모든 프로토콜이 동일한 이벤트 시스템 사용
- [x] **중앙집중식 스트림 관리**: MediaServer에서 모든 프로토콜의 스트림 통합 관리
- [x] **MediaFrame 통합 구조체**: 기존 Frame 시스템을 MediaFrame으로 리팩토링 완료

## TODO 리스트

### 🚀 곧 작업 해야할 TODO 리스트
- [ ] Stream에 Sink 없을 시 auto Release 구현
- [ ] rtmp video tag header Frame으로 전달시 제거 필요

### 고민중인 TODO 리스트
- [ ] keyframe 단위 캐시 on/off 기능 

### 향후 고려 해볼만한 TODO 리스트
- [ ] **프로토콜 확장**
   - [ ] HLS 출력 지원
   - [ ] WebRTC 지원 검토
   - [ ] SRT 지원 검토
   - [ ] MPEGTS 지원 검토
   - [ ] Transcoder 지원 검토


- to memorize