package srt

import (
	"context"
	"io"
	"log/slog"
	"sol/pkg/media"
	"sync"
	"sync/atomic"
	"time"

	gosrt "github.com/datarhei/gosrt"
)

// SessionMode SRT 세션 모드
type SessionMode int

const (
	ModePublish   SessionMode = iota // 발행 모드
	ModeSubscribe                    // 구독 모드
)

// Session SRT 세션 구조체
type Session struct {
	id                 uintptr
	conn               gosrt.Conn
	mode               SessionMode
	mediaServerChannel chan<- any
	wg                 *sync.WaitGroup

	// 스트리밍 관련
	stream      *media.Stream
	streamID    string
	streamPath  string
	mpegtsParser *MPEGTSParser

	// 상태 관리
	active    int32 // atomic
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
}

var sessionIDCounter uintptr

// NewSession 새로운 SRT 세션 생성
func NewSession(conn gosrt.Conn, mediaServerChannel chan<- any, wg *sync.WaitGroup) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	
	sessionID := atomic.AddUintptr(&sessionIDCounter, 1)
	
	session := &Session{
		id:                 sessionID,
		conn:               conn,
		mode:               ModePublish, // 기본값
		mediaServerChannel: mediaServerChannel,
		wg:                 wg,
		active:             1,
		ctx:                ctx,
		cancel:             cancel,
	}
	
	// MPEGTS 파서 초기화
	session.mpegtsParser = NewMPEGTSParser(session)
	
	return session
}

// MediaNode 인터페이스 구현

// ID 세션 ID 반환
func (s *Session) ID() uintptr {
	return s.id
}

// NodeType 노드 타입 반환
func (s *Session) NodeType() media.NodeType {
	return media.NodeTypeSRT
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
	var err error
	s.closeOnce.Do(func() {
		atomic.StoreInt32(&s.active, 0)
		s.cancel()
		
		if s.conn != nil {
			err = s.conn.Close()
		}
		
		slog.Info("SRT session closed", "sessionId", s.id, "address", s.Address())
	})
	return err
}

// isActive 세션 활성 상태 확인
func (s *Session) isActive() bool {
	return atomic.LoadInt32(&s.active) == 1
}

// MediaSource 인터페이스 구현 (발행 모드)

// PublishingStreams 발행 중인 스트림 목록 반환
func (s *Session) PublishingStreams() []*media.Stream {
	if s.mode == ModePublish && s.stream != nil {
		return []*media.Stream{s.stream}
	}
	return nil
}

// MediaSink 인터페이스 구현 (구독 모드)

// SendPacket 패킷 전송 (구독자에게)
func (s *Session) SendPacket(streamID string, packet media.Packet) error {
	if !s.isActive() || s.mode != ModeSubscribe {
		return nil
	}

	// TODO: SRT 패킷으로 변환하여 전송
	// 현재는 간단히 로그만 출력

	return nil
}

// SendMetadata 메타데이터 전송
func (s *Session) SendMetadata(streamID string, metadata map[string]string) error {
	if !s.isActive() || s.mode != ModeSubscribe {
		return nil
	}

	// TODO: SRT 메타데이터 전송

	return nil
}

// SubscribedStreams 구독 중인 스트림 목록 반환
func (s *Session) SubscribedStreams() []string {
	if s.mode == ModeSubscribe && s.streamID != "" {
		return []string{s.streamID}
	}
	return nil
}

// Run 세션 실행 (메인 루프)
func (s *Session) Run() error {
	s.wg.Add(1)
	defer s.wg.Done()
	
	// MediaServer에 노드 생성 이벤트 전송
	if s.mediaServerChannel != nil {
		select {
		case s.mediaServerChannel <- media.NodeCreated{
			ID:   s.ID(),
			Node: s,
		}:
		default:
		}
	}

	// 읽기 루프 시작
	buffer := make([]byte, 4096)
	
	for s.isActive() {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			// SRT 연결에서 데이터 읽기
			s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := s.conn.Read(buffer)
			if err != nil {
				if err == io.EOF {
					slog.Info("SRT connection closed by peer", "sessionId", s.id)
					return nil
				}
				
				// 타임아웃은 정상적인 상황
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					continue
				}
				
				slog.Error("SRT read error", "sessionId", s.id, "err", err)
				return err
			}

			if n > 0 {
				// MPEGTS 데이터 처리
				if err := s.processMPEGTSData(buffer[:n]); err != nil {
					slog.Error("Failed to process MPEGTS data", "sessionId", s.id, "err", err)
				}
			}
		}
	}

	return nil
}

// processMPEGTSData MPEGTS 데이터 처리
func (s *Session) processMPEGTSData(data []byte) error {
	// 첫 번째 데이터를 받으면 발행 모드로 설정
	if s.mode != ModePublish {
		s.mode = ModePublish
	}
	
	// MPEGTS 파서를 사용하여 데이터 처리
	return s.mpegtsParser.ParseMPEGTSData(data)
}