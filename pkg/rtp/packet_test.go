package rtp

import (
	"testing"
)

func TestRTPPacketMarshalUnmarshal(t *testing.T) {
	// Test data
	payload := []byte("Hello, RTP!")
	
	// Create RTP packet
	packet := NewRTPPacket(PayloadTypeH264, 12345, 98765432, 0x12345678, payload)
	packet.SetMarker(true)
	
	// Marshal
	data, err := packet.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal RTP packet: %v", err)
	}
	
	// Unmarshal
	packet2 := &RTPPacket{}
	err = packet2.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal RTP packet: %v", err)
	}
	
	// Verify
	if packet2.Header.Version != 2 {
		t.Errorf("Expected version 2, got %d", packet2.Header.Version)
	}
	
	if packet2.Header.PayloadType != PayloadTypeH264 {
		t.Errorf("Expected payload type %d, got %d", PayloadTypeH264, packet2.Header.PayloadType)
	}
	
	if packet2.Header.SequenceNumber != 12345 {
		t.Errorf("Expected sequence number 12345, got %d", packet2.Header.SequenceNumber)
	}
	
	if packet2.Header.Timestamp != 98765432 {
		t.Errorf("Expected timestamp 98765432, got %d", packet2.Header.Timestamp)
	}
	
	if packet2.Header.SSRC != 0x12345678 {
		t.Errorf("Expected SSRC 0x12345678, got 0x%x", packet2.Header.SSRC)
	}
	
	if !packet2.Header.Marker {
		t.Errorf("Expected marker bit to be true")
	}
	
	if string(packet2.Payload) != string(payload) {
		t.Errorf("Expected payload %s, got %s", string(payload), string(packet2.Payload))
	}
}

func TestRTPPacketTooBig(t *testing.T) {
	// Create packet that's too big
	bigPayload := make([]byte, MaxRTPPacketSize)
	
	packet := NewRTPPacket(PayloadTypeH264, 1, 1, 1, bigPayload)
	
	_, err := packet.Marshal()
	if err == nil {
		t.Errorf("Expected error for packet that's too big")
	}
}

func TestRTPPacketTooSmall(t *testing.T) {
	// Test with data that's too small
	smallData := []byte{0x80} // Only 1 byte
	
	packet := &RTPPacket{}
	err := packet.Unmarshal(smallData)
	if err == nil {
		t.Errorf("Expected error for packet that's too small")
	}
}

func TestRTPPacketStringRepresentation(t *testing.T) {
	packet := NewRTPPacket(PayloadTypeH264, 12345, 98765, 0x12345678, []byte("test"))
	
	str := packet.String()
	expected := "RTP{V:2 PT:96 Seq:12345 TS:98765 SSRC:305419896 PayloadLen:4}"
	
	if str != expected {
		t.Errorf("Expected string representation %s, got %s", expected, str)
	}
}
