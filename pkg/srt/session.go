package srt

import (
	"context"
	"fmt"
	"log/slog"
	"sol/pkg/core"
	"sol/pkg/utils"
	"sync"
	"sync/atomic"

	gosrt "github.com/datarhei/gosrt"
)

var sessionIDCounter uint64

// 이벤트 타입 정의 (RTMP 패턴과 동일)
type rawDataEvent struct {
	data []byte
}

// Session SRT 세션 구조체
type Session struct {
	id                 uint64
	conn               gosrt.Conn
	mediaServerChannel chan<- any
	wg                 *sync.WaitGroup

	// 스트리밍 관련 (SRT는 연결당 하나의 스트림만)
	publishedStream  *core.Stream // 발행 중인 스트림 (하나만)
	subscribedStream string       // 구독 중인 스트림 ID (하나만)

	// 상태 관리
	channel chan any // 이벤트 채널 (RTMP 패턴과 동일)
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewSession 새로운 SRT 세션 생성 (RTMP 패턴과 동일)
func NewSession(conn gosrt.Conn, mediaServerChannel chan<- any, wg *sync.WaitGroup) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	sessionID := atomic.AddUint64(&sessionIDCounter, 1)

	session := &Session{
		id:                 sessionID,
		conn:               conn,
		mediaServerChannel: mediaServerChannel,
		wg:                 wg,
		publishedStream:    nil,                 // 처음에는 발행하지 않음
		subscribedStream:   "",                  // 처음에는 구독하지 않음
		channel:            make(chan any, 100), // 이벤트 채널 (버퍼 100)
		ctx:                ctx,
		cancel:             cancel,
	}

	// 세션의 두 주요 고루틴 시작 (RTMP 패턴과 동일)
	go session.eventLoop()
	go session.readLoop()

	return session
}

// --- MediaNode 인터페이스 구현 ---

// ID 세션 ID 반환
func (s *Session) ID() uintptr {
	return uintptr(s.id)
}

// NodeType 노드 타입 반환
func (s *Session) NodeType() core.NodeType {
	return core.NodeTypeSRT
}

// Address 클라이언트 주소 반환
func (s *Session) Address() string {
	if s.conn != nil {
		return s.conn.RemoteAddr().String()
	}
	return "unknown"
}

// Close 세션 종료
func (s *Session) Close() error {
	s.cancel()
	return nil
}

// cleanup 세션 정리 (RTMP 패턴과 동일)
func (s *Session) cleanup() {
	utils.CloseWithLog(s.conn)

	// MediaServer에 종료 이벤트 전송 (RTMP와 동일)
	s.mediaServerChannel <- core.NewNodeTerminated(s.ID())

}

// isActive 세션 활성 상태 확인
func (s *Session) isActive() bool {
	select {
	case <-s.ctx.Done():
		return false
	default:
		return true
	}
}

// --- MediaSource 인터페이스 구현 (발행 모드) ---

// PublishingStreams 발행 중인 스트림 목록 반환
func (s *Session) PublishingStreams() []*core.Stream {
	if s.publishedStream == nil {
		return nil
	}

	return []*core.Stream{s.publishedStream}
}

// --- MediaSink 인터페이스 구현 (구독 모드) ---

// SendPacket 패킷 전송 (구독자에게)
func (s *Session) SendPacket(streamID string, packet core.Packet) error {
	if !s.isActive() {
		return nil
	}

	// 구독 중인 스트림인지 확인 (SRT는 하나의 스트림만)
	if s.subscribedStream != streamID {
		return nil // 구독하지 않은 스트림은 무시
	}

	// TODO: SRT 패킷으로 변환하여 전송
	// 현재는 MPEGTS 처리를 하지 않으므로 단순히 로그만 출력
	slog.Debug("SRT session received packet", "sessionId", s.id, "streamId", streamID, "codec", packet.Codec, "type", packet.Type)

	return nil
}

// SendMetadata 메타데이터 전송
func (s *Session) SendMetadata(streamID string, metadata map[string]string) error {
	if !s.isActive() {
		return nil
	}

	// TODO: SRT 메타데이터 전송
	slog.Debug("SRT session received metadata", "sessionId", s.id, "streamId", streamID, "metadata", metadata)

	return nil
}

// SubscribedStreams 구독 중인 스트림 목록 반환
func (s *Session) SubscribedStreams() []string {
	if s.subscribedStream == "" {
		return nil
	}
	return []string{s.subscribedStream}
}

// --- 세션 실행 (RTMP 패턴) ---

// eventLoop 세션의 모든 상태 변경과 I/O 쓰기를 처리하는 메인 루프 (RTMP 패턴과 동일)
func (s *Session) eventLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer s.cleanup()

	// MediaServer에 노드 생성 이벤트 전송
	if s.mediaServerChannel != nil {
		select {
		case s.mediaServerChannel <- core.NodeCreated{
			ID:   s.ID(),
			Node: s,
		}:
		default:
		}
	}

	for {
		select {
		case data := <-s.channel:
			s.handleChannelEvent(data)
		case <-s.ctx.Done():
			return
		}
	}
}

// readLoop SRT 연결에서 데이터를 읽는 루프 (RTMP 패턴과 동일)
func (s *Session) readLoop() {
	s.wg.Add(1)
	defer s.wg.Done()
	defer s.cancel()

	// 읽기 루프 시작
	buffer := make([]byte, 4096)

	for s.isActive() {
		select {
		case <-s.ctx.Done():
			return
		default:
			// SRT 연결에서 데이터 읽기 (RTMP처럼 단순하게)
			n, err := s.conn.Read(buffer)
			if err != nil {
				slog.Error("SRT read error", "sessionId", s.id, "err", err)
				return
			}

			if n > 0 {
				// 읽은 데이터를 이벤트 채널로 전송
				select {
				case s.channel <- rawDataEvent{data: buffer[:n]}:
				default:
					slog.Warn("Event channel full, dropping data", "sessionId", s.id)
				}
			}
		}
	}
}

// handleChannelEvent 채널 이벤트 처리 (RTMP 패턴과 동일)
func (s *Session) handleChannelEvent(data any) {
	switch event := data.(type) {
	case rawDataEvent:
		s.processRawData(event.data)
	default:
		slog.Error("Unknown event type", "type", fmt.Sprintf("%T", event))
	}
}

// processRawData 원시 데이터 처리 (현재는 로그만)
func (s *Session) processRawData(data []byte) {
	// 데이터를 받았다는 것은 발행자라는 의미
	// (실제 스트림 생성은 MPEGTS 파싱 후에)
	slog.Debug("SRT session received raw data", "sessionId", s.id, "size", len(data))

	// TODO: 향후 gots 라이브러리를 사용하여 MPEGTS 파싱 구현
	// TODO: 파싱 후 publishedStream에 스트림 설정 (하나만)
}
