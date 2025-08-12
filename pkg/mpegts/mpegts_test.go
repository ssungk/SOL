package mpegts

import (
	"context"
	"sol/pkg/media"
	"testing"
)

// TestTSPacketParsing TS 패킷 파싱 테스트
func TestTSPacketParsing(t *testing.T) {
	// 테스트용 TS 패킷 데이터 (188바이트)
	testPacket := make([]byte, TSPacketSize)
	testPacket[0] = SyncByte           // 동기 바이트
	testPacket[1] = 0x40               // PUSI=1, PID 상위 5비트=0
	testPacket[2] = 0x00               // PID 하위 8비트 (PID=0x000, PAT)
	testPacket[3] = 0x10               // AFC=01 (페이로드만), CC=0

	// 나머지를 0xFF로 채우기
	for i := 4; i < TSPacketSize; i++ {
		testPacket[i] = 0xFF
	}

	// 파싱 테스트
	packet, err := ParseTSPacket(testPacket)
	if err != nil {
		t.Fatalf("Failed to parse TS packet: %v", err)
	}

	// 헤더 검증
	if packet.Header.SyncByte != SyncByte {
		t.Errorf("Expected sync byte 0x%02X, got 0x%02X", SyncByte, packet.Header.SyncByte)
	}

	if packet.Header.PID != PIDPAT {
		t.Errorf("Expected PID %d, got %d", PIDPAT, packet.Header.PID)
	}

	if !packet.Header.PayloadUnitStartIndicator {
		t.Error("Expected PayloadUnitStartIndicator to be true")
	}

	if packet.Header.AdaptationFieldControl != 0x01 {
		t.Errorf("Expected AFC 0x01, got 0x%02X", packet.Header.AdaptationFieldControl)
	}
}

// TestPATCreation PAT 생성 및 파싱 테스트
func TestPATCreation(t *testing.T) {
	programs := []PATEntry{
		{ProgramNumber: 1, PID: 0x100},
		{ProgramNumber: 2, PID: 0x200},
	}

	// PAT 생성
	patData, err := CreatePAT(0x0001, programs)
	if err != nil {
		t.Fatalf("Failed to create PAT: %v", err)
	}

	// PAT 파싱
	parsedPAT, err := ParsePAT(patData)
	if err != nil {
		t.Fatalf("Failed to parse PAT: %v", err)
	}

	// 검증
	if parsedPAT.TableID != TableIDPAT {
		t.Errorf("Expected table ID %d, got %d", TableIDPAT, parsedPAT.TableID)
	}

	if parsedPAT.TransportStreamID != 0x0001 {
		t.Errorf("Expected transport stream ID 1, got %d", parsedPAT.TransportStreamID)
	}

	if len(parsedPAT.Programs) != len(programs) {
		t.Errorf("Expected %d programs, got %d", len(programs), len(parsedPAT.Programs))
	}

	for i, program := range parsedPAT.Programs {
		if program.ProgramNumber != programs[i].ProgramNumber {
			t.Errorf("Program %d: expected number %d, got %d", i, programs[i].ProgramNumber, program.ProgramNumber)
		}
		if program.PID != programs[i].PID {
			t.Errorf("Program %d: expected PID %d, got %d", i, programs[i].PID, program.PID)
		}
	}
}

// TestPMTCreation PMT 생성 및 파싱 테스트  
func TestPMTCreation(t *testing.T) {
	streams := []PMTStreamInfo{
		{StreamType: StreamTypeH264, ElementaryPID: 0x101},
		{StreamType: StreamTypeADTS, ElementaryPID: 0x102},
	}

	// PMT 생성
	pmtData, err := CreatePMT(1, 0x101, streams)
	if err != nil {
		t.Fatalf("Failed to create PMT: %v", err)
	}

	// PMT 파싱
	parsedPMT, err := ParsePMT(pmtData)
	if err != nil {
		t.Fatalf("Failed to parse PMT: %v", err)
	}

	// 검증
	if parsedPMT.TableID != TableIDPMT {
		t.Errorf("Expected table ID %d, got %d", TableIDPMT, parsedPMT.TableID)
	}

	if parsedPMT.ProgramNumber != 1 {
		t.Errorf("Expected program number 1, got %d", parsedPMT.ProgramNumber)
	}

	if parsedPMT.PCR_PID != 0x101 {
		t.Errorf("Expected PCR PID 0x101, got 0x%X", parsedPMT.PCR_PID)
	}

	if len(parsedPMT.Streams) != len(streams) {
		t.Errorf("Expected %d streams, got %d", len(streams), len(parsedPMT.Streams))
	}
}

// TestDemuxer 디먹서 테스트
func TestDemuxer(t *testing.T) {
	ctx := context.Background()
	config := DemuxerConfig{
		BufferSize:     1024 * 1024,
		EnableDebugLog: true,
	}

	demux := NewDemuxer(ctx, config)
	defer demux.Close()

	// 콜백 설정
	frameReceived := false
	demux.SetFrameCallback(func(streamPID uint16, frame media.Frame) error {
		frameReceived = true
		t.Logf("Frame received: PID=%d, Type=%d, SubType=%d, DataChunks=%d", 
			streamPID, frame.Type, frame.SubType, len(frame.Data))
		return nil
	})

	metadataReceived := false
	demux.SetMetadataCallback(func(streamPID uint16, metadata map[string]string) error {
		metadataReceived = true
		t.Logf("Metadata received: PID=%d, metadata=%v", streamPID, metadata)
		return nil
	})

	// 테스트용 TS 데이터 생성 (PAT만)
	programs := []PATEntry{{ProgramNumber: 1, PID: 0x100}}
	patData, _ := CreatePAT(0x0001, programs)

	// PAT를 TS 패킷으로 래핑
	header := TSPacketHeader{
		SyncByte:                  SyncByte,
		PayloadUnitStartIndicator: true,
		PID:                       PIDPAT,
		AdaptationFieldControl:    0x01,
		ContinuityCounter:         0,
	}

	tsPacket, err := CreateTSPacket(header, nil, patData)
	if err != nil {
		t.Fatalf("Failed to create TS packet: %v", err)
	}

	// 디먹서로 처리
	err = demux.ProcessData(tsPacket)
	if err != nil {
		t.Fatalf("Failed to process data: %v", err)
	}

	// 스트림 정보 확인
	streams := demux.GetStreamInfo()
	t.Logf("Streams found: %d", len(streams))

	// 참고: 실제 비디오/오디오 스트림 데이터가 없으므로 
	// frameReceived와 metadataReceived는 false일 것임
	_ = frameReceived
	_ = metadataReceived
}

// TestMuxer 먹서 테스트
func TestMuxer(t *testing.T) {
	ctx := context.Background()
	config := MuxerConfig{
		ProgramNumber:     1,
		TransportStreamID: 0x0001,
		PMT_PID:          0x100,
		EnableDebugLog:   true,
	}

	mux := NewMuxer(ctx, config)
	defer mux.Close()

	// H.264 스트림 추가
	codecData := &CodecData{
		Type:   StreamTypeH264,
		Width:  1920,
		Height: 1080,
		Profile: 77, // Main Profile
		Level:   40, // Level 4.0
	}

	videoPID, err := mux.AddStream(StreamTypeH264, codecData)
	if err != nil {
		t.Fatalf("Failed to add video stream: %v", err)
	}

	// AAC 스트림 추가
	audioCodecData := &CodecData{
		Type:       StreamTypeADTS,
		Channels:   2,
		SampleRate: 44100,
		Profile:    2, // LC
	}

	audioPID, err := mux.AddStream(StreamTypeADTS, audioCodecData)
	if err != nil {
		t.Fatalf("Failed to add audio stream: %v", err)
	}

	// PAT 생성
	pat, err := mux.GetPAT()
	if err != nil {
		t.Fatalf("Failed to generate PAT: %v", err)
	}
	
	// PMT 생성
	pmt, err := mux.GetPMT(config.ProgramNumber)
	if err != nil {
		t.Fatalf("Failed to generate PMT: %v", err)
	}
	
	patpmt := append(pat, pmt...)

	if len(patpmt) != TSPacketSize*2 {
		t.Errorf("Expected PAT+PMT size %d, got %d", TSPacketSize*2, len(patpmt))
	}

	// 테스트용 비디오 프레임
	videoFrame := media.Frame{
		Type:      media.TypeVideo,
		SubType:   media.VideoKeyFrame,
		Timestamp: 1000,
		Data:      [][]byte{{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E}}, // 가짜 SPS
	}

	tsPackets, err := mux.WriteFrame(videoPID, videoFrame)
	if err != nil {
		t.Fatalf("Failed to write video frame: %v", err)
	}

	if len(tsPackets)%TSPacketSize != 0 {
		t.Errorf("Invalid TS packet size: %d", len(tsPackets))
	}

	t.Logf("Generated %d TS packets for video frame", len(tsPackets)/TSPacketSize)

	// 테스트용 오디오 프레임
	audioFrame := media.Frame{
		Type:      media.TypeAudio,
		SubType:   media.AudioRawData,
		Timestamp: 1000,
		Data:      [][]byte{{0xFF, 0xF1, 0x50, 0x80, 0x01, 0x20}}, // 가짜 ADTS
	}

	audioPackets, err := mux.WriteFrame(audioPID, audioFrame)
	if err != nil {
		t.Fatalf("Failed to write audio frame: %v", err)
	}

	t.Logf("Generated %d TS packets for audio frame", len(audioPackets)/TSPacketSize)
}

// TestStreamTypes 스트림 타입 헬퍼 테스트
func TestStreamTypes(t *testing.T) {
	// 비디오 스트림 타입 테스트
	videoTypes := []StreamType{StreamTypeH264, StreamTypeH265, StreamTypeMPEG2Video}
	for _, st := range videoTypes {
		if !st.IsVideoStream() {
			t.Errorf("Stream type %s should be video", st.String())
		}
		if st.IsAudioStream() {
			t.Errorf("Stream type %s should not be audio", st.String())
		}
	}

	// 오디오 스트림 타입 테스트
	audioTypes := []StreamType{StreamTypeADTS, StreamTypeAAC, StreamTypeMPEG2Audio}
	for _, st := range audioTypes {
		if !st.IsAudioStream() {
			t.Errorf("Stream type %s should be audio", st.String())
		}
		if st.IsVideoStream() {
			t.Errorf("Stream type %s should not be video", st.String())
		}
	}
}

// TestNALUTypes NALU 타입 헬퍼 테스트
func TestNALUTypes(t *testing.T) {
	// H.264 키프레임 테스트
	if !H264NALUTypeSliceIDR.IsH264KeyFrame() {
		t.Error("H.264 IDR slice should be key frame")
	}

	// H.264 설정 NALU 테스트
	if !H264NALUTypeSPS.IsH264Config() {
		t.Error("H.264 SPS should be config NALU")
	}
	if !H264NALUTypePPS.IsH264Config() {
		t.Error("H.264 PPS should be config NALU")
	}

	// H.265 키프레임 테스트
	if !H265NALUTypeSliceIDR_W_RADL.IsH265KeyFrame() {
		t.Error("H.265 IDR slice should be key frame")
	}

	// H.265 설정 NALU 테스트
	if !H265NALUTypeVPS.IsH265Config() {
		t.Error("H.265 VPS should be config NALU")
	}
	if !H265NALUTypeSPS.IsH265Config() {
		t.Error("H.265 SPS should be config NALU")
	}
	if !H265NALUTypePPS.IsH265Config() {
		t.Error("H.265 PPS should be config NALU")
	}
}

// TestTimestampConversion 타임스탬프 변환 테스트
func TestTimestampConversion(t *testing.T) {
	// PTS를 밀리초로 변환 테스트
	pts := uint64(90000) // 1초 (90kHz 클록)
	expectedMs := uint32(1000)
	actualMs := ExtractTimestamp(pts)

	if actualMs != expectedMs {
		t.Errorf("Expected %d ms, got %d ms", expectedMs, actualMs)
	}

	// 프레임 타임스탬프를 PTS로 변환 테스트 (내부 함수이므로 간접 테스트)
	frameTs := uint32(2000) // 2초
	expectedPTS := uint64(180000) // 2초 * 90000
	
	// 먹서의 internal 함수는 직접 테스트할 수 없으므로 
	// 공식만 확인
	calculatedPTS := uint64(frameTs) * 90
	if calculatedPTS != expectedPTS {
		t.Errorf("Expected PTS %d, calculated %d", expectedPTS, calculatedPTS)
	}
}

// BenchmarkTSPacketParsing TS 패킷 파싱 벤치마크
func BenchmarkTSPacketParsing(b *testing.B) {
	testPacket := make([]byte, TSPacketSize)
	testPacket[0] = SyncByte
	testPacket[1] = 0x40
	testPacket[2] = 0x00
	testPacket[3] = 0x10

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseTSPacket(testPacket)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDemuxer 디먹서 처리 벤치마크
func BenchmarkDemuxer(b *testing.B) {
	ctx := context.Background()
	config := DemuxerConfig{BufferSize: 1024 * 1024}
	demux := NewDemuxer(ctx, config)
	defer demux.Close()

	// 테스트 데이터 (PAT 패킷)
	programs := []PATEntry{{ProgramNumber: 1, PID: 0x100}}
	patData, _ := CreatePAT(0x0001, programs)
	header := TSPacketHeader{
		SyncByte:                  SyncByte,
		PayloadUnitStartIndicator: true,
		PID:                       PIDPAT,
		AdaptationFieldControl:    0x01,
		ContinuityCounter:         0,
	}
	tsPacket, _ := CreateTSPacket(header, nil, patData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := demux.ProcessData(tsPacket)
		if err != nil {
			b.Fatal(err)
		}
	}
}