package srt

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// ParseUDTPacket 원시 바이트에서 UDT 패킷 파싱
func ParseUDTPacket(data []byte) (*UDTPacket, error) {
	if len(data) < UDTHeaderSize {
		return nil, fmt.Errorf("packet too small: %d bytes, expected at least %d", len(data), UDTHeaderSize)
	}
	
	header := UDTHeader{
		SeqNum:        binary.BigEndian.Uint32(data[0:4]),
		MessageNumber: binary.BigEndian.Uint32(data[4:8]),
		Timestamp:     binary.BigEndian.Uint32(data[8:12]),
		DestSocketID:  binary.BigEndian.Uint32(data[12:16]),
	}
	
	var payload []byte
	if len(data) > UDTHeaderSize {
		payload = data[UDTHeaderSize:]
	}
	
	return &UDTPacket{
		Header:  header,
		Payload: payload,
	}, nil
}

// SerializeUDTHeader UDT 헤더를 바이트로 직렬화
func SerializeUDTHeader(header *UDTHeader) []byte {
	data := make([]byte, UDTHeaderSize)
	
	binary.BigEndian.PutUint32(data[0:4], header.SeqNum)
	binary.BigEndian.PutUint32(data[4:8], header.MessageNumber)
	binary.BigEndian.PutUint32(data[8:12], header.Timestamp)
	binary.BigEndian.PutUint32(data[12:16], header.DestSocketID)
	
	return data
}

// CreateControlPacket 제어 패킷 생성
func CreateControlPacket(controlType ControlPacketType, socketID uint32, additionalInfo uint32) []byte {
	header := UDTHeader{
		SeqNum:        uint32(controlType) | 0x80000000, // MSB를 1로 설정하여 제어 패킷 표시
		MessageNumber: additionalInfo,
		Timestamp:     uint32(time.Now().UnixNano() / 1000), // 마이크로초
		DestSocketID:  socketID,
	}
	
	return SerializeUDTHeader(&header)
}

// CreateDataPacket 데이터 패킷 생성
func CreateDataPacket(seqNum uint32, messageNum uint32, socketID uint32, payload []byte) []byte {
	header := UDTHeader{
		SeqNum:        seqNum & 0x7FFFFFFF, // MSB를 0으로 설정하여 데이터 패킷 표시
		MessageNumber: messageNum,
		Timestamp:     uint32(time.Now().UnixNano() / 1000), // 마이크로초
		DestSocketID:  socketID,
	}
	
	headerBytes := SerializeUDTHeader(&header)
	result := make([]byte, len(headerBytes)+len(payload))
	copy(result, headerBytes)
	copy(result[len(headerBytes):], payload)
	
	return result
}

// ParseSRTHandshake SRT 핸드셰이크 패킷 파싱
func ParseSRTHandshake(data []byte) (*SRTHandshakePacket, error) {
	if len(data) < 48 { // 최소 핸드셰이크 크기
		return nil, fmt.Errorf("handshake packet too small: %d bytes", len(data))
	}
	
	// UDT 핸드셰이크 부분 파싱
	handshake := &SRTHandshakePacket{
		UDTVersion:     binary.BigEndian.Uint32(data[16:20]), // UDT 헤더 이후
		SocketType:     binary.BigEndian.Uint32(data[20:24]),
		InitPacketSeq:  binary.BigEndian.Uint32(data[24:28]),
		MaxPacketSize:  binary.BigEndian.Uint32(data[28:32]),
		MaxFlowWinSize: binary.BigEndian.Uint32(data[32:36]),
		HandshakeType:  binary.BigEndian.Uint32(data[36:40]),
		SocketID:       binary.BigEndian.Uint32(data[40:44]),
		SynCookie:      binary.BigEndian.Uint32(data[44:48]),
	}
	
	// 피어 IP 주소 복사
	if len(data) >= 64 {
		copy(handshake.PeerIPAddress[:], data[48:64])
	}
	
	// SRT 확장 부분이 있는지 확인
	if len(data) >= 72 {
		handshake.SRTFlags = binary.BigEndian.Uint32(data[64:68])
		handshake.RecvTSBPDDelay = binary.BigEndian.Uint16(data[68:70])
		handshake.SendTSBPDDelay = binary.BigEndian.Uint16(data[70:72])
	}
	
	return handshake, nil
}

// CreateSRTHandshakeResponse SRT 핸드셰이크 응답 생성
func CreateSRTHandshakeResponse(req *SRTHandshakePacket, serverSocketID uint32, latency uint16) []byte {
	// 기본 UDT 헤더 (16바이트)
	header := UDTHeader{
		SeqNum:        uint32(ControlHandshake) | 0x80000000,
		MessageNumber: 0,
		Timestamp:     uint32(time.Now().UnixNano() / 1000),
		DestSocketID:  req.SocketID,
	}
	
	// 핸드셰이크 페이로드 (최소 56바이트)
	payload := make([]byte, 56)
	
	// UDT 핸드셰이크 응답
	binary.BigEndian.PutUint32(payload[0:4], 4)                    // UDT 버전
	binary.BigEndian.PutUint32(payload[4:8], 1)                    // 소켓 타입 (스트림)
	binary.BigEndian.PutUint32(payload[8:12], req.InitPacketSeq)   // 초기 시퀀스 번호
	binary.BigEndian.PutUint32(payload[12:16], req.MaxPacketSize)  // 최대 패킷 크기
	binary.BigEndian.PutUint32(payload[16:20], req.MaxFlowWinSize) // 최대 플로우 윈도우
	binary.BigEndian.PutUint32(payload[20:24], 1)                  // 핸드셰이크 응답
	binary.BigEndian.PutUint32(payload[24:28], serverSocketID)     // 서버 소켓 ID
	binary.BigEndian.PutUint32(payload[28:32], req.SynCookie)      // SYN 쿠키 반환
	
	// 피어 IP 복사
	copy(payload[32:48], req.PeerIPAddress[:])
	
	// SRT 확장
	binary.BigEndian.PutUint32(payload[48:52], 0x00000001) // SRT 플래그 (SRT 활성화)
	binary.BigEndian.PutUint16(payload[52:54], latency)    // 수신 지연
	binary.BigEndian.PutUint16(payload[54:56], latency)    // 송신 지연
	
	// 헤더와 페이로드 결합
	result := make([]byte, UDTHeaderSize+len(payload))
	copy(result, SerializeUDTHeader(&header))
	copy(result[UDTHeaderSize:], payload)
	
	return result
}

// ExtractClientIP 클라이언트 IP 주소 추출
func ExtractClientIP(addr *net.UDPAddr) [16]byte {
	var ipBytes [16]byte
	
	if ip4 := addr.IP.To4(); ip4 != nil {
		// IPv4는 마지막 4바이트에 저장
		copy(ipBytes[12:], ip4)
	} else if ip6 := addr.IP.To16(); ip6 != nil {
		// IPv6는 전체 16바이트 사용
		copy(ipBytes[:], ip6)
	}
	
	return ipBytes
}

// IsHandshakePacket 핸드셰이크 패킷인지 확인
func IsHandshakePacket(packet *UDTPacket) bool {
	return packet.Header.IsControlPacket() && 
		   packet.Header.GetControlType() == ControlHandshake
}

// IsKeepAlivePacket 킵얼라이브 패킷인지 확인  
func IsKeepAlivePacket(packet *UDTPacket) bool {
	return packet.Header.IsControlPacket() &&
		   packet.Header.GetControlType() == ControlKeepAlive
}

// CreateKeepAlivePacket 킵얼라이브 패킷 생성
func CreateKeepAlivePacket(socketID uint32) []byte {
	return CreateControlPacket(ControlKeepAlive, socketID, 0)
}

// CreateAckPacket ACK 패킷 생성
func CreateAckPacket(socketID uint32, ackSeqNum uint32) []byte {
	return CreateControlPacket(ControlAck, socketID, ackSeqNum)
}

// CreateNakPacket NAK 패킷 생성
func CreateNakPacket(socketID uint32, lostSeqNum uint32) []byte {
	return CreateControlPacket(ControlNak, socketID, lostSeqNum)
}