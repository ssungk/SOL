package rtp

import (
	"encoding/binary"
	"fmt"
)

// RTPHeader represents the RTP packet header
type RTPHeader struct {
	Version        uint8  // 2 bits: Version (V)
	Padding        bool   // 1 bit: Padding (P)
	Extension      bool   // 1 bit: Extension (X)
	CSRCCount      uint8  // 4 bits: CSRC count (CC)
	Marker         bool   // 1 bit: Marker (M)
	PayloadType    uint8  // 7 bits: Payload type (PT)
	SequenceNumber uint16 // 16 bits: Sequence number
	Timestamp      uint32 // 32 bits: Timestamp
	SSRC           uint32 // 32 bits: SSRC identifier
}

// RTPPacket represents a complete RTP packet
type RTPPacket struct {
	Header  *RTPHeader
	Payload []byte
}

// Constants for RTP
const (
	MinRTPHeaderSize = 12   // Minimum RTP header size in bytes
	MaxRTPPacketSize = 1500 // Maximum RTP packet size (MTU)
)

// Common payload types
const (
	PayloadTypeH264 = 96  // H.264 (dynamic)
	PayloadTypeAAC  = 97  // AAC (dynamic)
)

// NewRTPPacket creates a new RTP packet
func NewRTPPacket(payloadType uint8, sequenceNumber uint16, timestamp uint32, ssrc uint32, payload []byte) *RTPPacket {
	return &RTPPacket{
		Header: &RTPHeader{
			Version:        2,
			Padding:        false,
			Extension:      false,
			CSRCCount:      0,
			Marker:         false,
			PayloadType:    payloadType,
			SequenceNumber: sequenceNumber,
			Timestamp:      timestamp,
			SSRC:           ssrc,
		},
		Payload: payload,
	}
}

// Marshal serializes the RTP packet to bytes
func (p *RTPPacket) Marshal() ([]byte, error) {
	totalSize := MinRTPHeaderSize + len(p.Payload)
	
	if totalSize > MaxRTPPacketSize {
		return nil, fmt.Errorf("RTP packet too large: %d bytes (max: %d)", totalSize, MaxRTPPacketSize)
	}
	
	buf := make([]byte, totalSize)
	
	// First byte: V(2) + P(1) + X(1) + CC(4)
	buf[0] = (p.Header.Version << 6) | 
		     (boolToBit(p.Header.Padding) << 5) |
		     (boolToBit(p.Header.Extension) << 4) |
		     p.Header.CSRCCount
	
	// Second byte: M(1) + PT(7)
	buf[1] = (boolToBit(p.Header.Marker) << 7) | p.Header.PayloadType
	
	// Sequence number (16 bits)
	binary.BigEndian.PutUint16(buf[2:4], p.Header.SequenceNumber)
	
	// Timestamp (32 bits)
	binary.BigEndian.PutUint32(buf[4:8], p.Header.Timestamp)
	
	// SSRC (32 bits)
	binary.BigEndian.PutUint32(buf[8:12], p.Header.SSRC)
	
	// Payload
	copy(buf[12:], p.Payload)
	
	return buf, nil
}

// Unmarshal deserializes bytes to RTP packet
func (p *RTPPacket) Unmarshal(data []byte) error {
	if len(data) < MinRTPHeaderSize {
		return fmt.Errorf("RTP packet too short: %d bytes (min: %d)", len(data), MinRTPHeaderSize)
	}
	
	p.Header = &RTPHeader{}
	
	// First byte: V(2) + P(1) + X(1) + CC(4)
	firstByte := data[0]
	p.Header.Version = (firstByte >> 6) & 0x03
	p.Header.Padding = (firstByte >> 5) & 0x01 == 1
	p.Header.Extension = (firstByte >> 4) & 0x01 == 1
	p.Header.CSRCCount = firstByte & 0x0F
	
	// Second byte: M(1) + PT(7)
	secondByte := data[1]
	p.Header.Marker = (secondByte >> 7) & 0x01 == 1
	p.Header.PayloadType = secondByte & 0x7F
	
	// Sequence number (16 bits)
	p.Header.SequenceNumber = binary.BigEndian.Uint16(data[2:4])
	
	// Timestamp (32 bits)
	p.Header.Timestamp = binary.BigEndian.Uint32(data[4:8])
	
	// SSRC (32 bits)
	p.Header.SSRC = binary.BigEndian.Uint32(data[8:12])
	
	// Payload
	p.Payload = make([]byte, len(data)-MinRTPHeaderSize)
	copy(p.Payload, data[MinRTPHeaderSize:])
	
	return nil
}

// SetMarker sets the marker bit
func (p *RTPPacket) SetMarker(marker bool) {
	p.Header.Marker = marker
}

// String returns a string representation of the RTP packet
func (p *RTPPacket) String() string {
	return fmt.Sprintf("RTP{V:%d PT:%d Seq:%d TS:%d SSRC:%d PayloadLen:%d}",
		p.Header.Version,
		p.Header.PayloadType,
		p.Header.SequenceNumber,
		p.Header.Timestamp,
		p.Header.SSRC,
		len(p.Payload))
}

// boolToBit converts boolean to bit (0 or 1)
func boolToBit(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
