package fmp4

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sol/pkg/media"
	"time"
)

// FMP4Writer fMP4 세그먼트 작성기
type FMP4Writer struct {
	// 기본 정보
	sequenceNumber uint32            // 세그먼트 시퀀스 번호
	timescale      uint32            // 타임스케일
	
	// 트랙 정보
	videoTrack     *TrackInfo        // 비디오 트랙 정보
	audioTrack     *TrackInfo        // 오디오 트랙 정보
	
	// 초기화 세그먼트 캐시
	initSegment    *InitializationSegment
	
	// 출력 버퍼
	buffer         *bytes.Buffer
}

// TrackInfo 트랙 정보 구조
type TrackInfo struct {
	ID              uint32    // 트랙 ID
	Type            string    // 트랙 타입 (video/audio)
	Timescale       uint32    // 타임스케일
	Duration        uint32    // 지속시간
	Width           uint16    // 너비 (비디오만)
	Height          uint16    // 높이 (비디오만)
	SampleRate      uint32    // 샘플 레이트 (오디오만)
	Channels        uint16    // 채널 수 (오디오만)
	CodecConfig     []byte    // 코덱 설정 데이터
}

// NewFMP4Writer 새 fMP4 작성기 생성
func NewFMP4Writer() *FMP4Writer {
	return &FMP4Writer{
		sequenceNumber: 0,
		timescale:     MovieTimeScale,
		buffer:        &bytes.Buffer{},
	}
}

// WriteInitializationSegment 초기화 세그먼트 작성
func (w *FMP4Writer) WriteInitializationSegment(hasVideo, hasAudio bool) ([]byte, error) {
	w.buffer.Reset()
	
	// FTYP 박스 작성
	if err := w.writeFTYP(); err != nil {
		return nil, fmt.Errorf("failed to write FTYP: %w", err)
	}
	
	// MOOV 박스 작성
	if err := w.writeMOOV(hasVideo, hasAudio); err != nil {
		return nil, fmt.Errorf("failed to write MOOV: %w", err)
	}
	
	// 초기화 세그먼트 캐시
	w.initSegment = &InitializationSegment{
		FTYP: w.buffer.Bytes(),
		MOOV: []byte{}, // MOOV는 별도로 캐시하지 않음 (크기 때문에)
	}
	
	return w.buffer.Bytes(), nil
}

// WriteMediaSegment 미디어 세그먼트 작성
func (w *FMP4Writer) WriteMediaSegment(frames []media.Frame) ([]byte, error) {
	w.buffer.Reset()
	w.sequenceNumber++
	
	// 프래그먼트 생성
	fragment, err := w.createFragment(frames)
	if err != nil {
		return nil, fmt.Errorf("failed to create fragment: %w", err)
	}
	
	// MOOF 박스 작성
	if err := w.writeMOOF(fragment); err != nil {
		return nil, fmt.Errorf("failed to write MOOF: %w", err)
	}
	
	// MDAT 박스 작성
	if err := w.writeMDAT(fragment); err != nil {
		return nil, fmt.Errorf("failed to write MDAT: %w", err)
	}
	
	return w.buffer.Bytes(), nil
}

// createFragment 프레임들로부터 프래그먼트 생성
func (w *FMP4Writer) createFragment(frames []media.Frame) (*Fragment, error) {
	fragment := &Fragment{
		SequenceNumber: w.sequenceNumber,
		TrackFragments: make([]TrackFragment, 0, 2), // video + audio
	}
	
	// 프레임들을 트랙별로 분리
	videoFrames := make([]media.Frame, 0)
	audioFrames := make([]media.Frame, 0)
	
	for _, frame := range frames {
		switch frame.Type {
		case media.TypeVideo:
			videoFrames = append(videoFrames, frame)
		case media.TypeAudio:
			audioFrames = append(audioFrames, frame)
		}
	}
	
	// 비디오 트랙 프래그먼트 생성
	if len(videoFrames) > 0 {
		videoTraf := w.createTrackFragment(VideoTrackID, videoFrames)
		fragment.TrackFragments = append(fragment.TrackFragments, videoTraf)
	}
	
	// 오디오 트랙 프래그먼트 생성  
	if len(audioFrames) > 0 {
		audioTraf := w.createTrackFragment(AudioTrackID, audioFrames)
		fragment.TrackFragments = append(fragment.TrackFragments, audioTraf)
	}
	
	return fragment, nil
}

// createTrackFragment 트랙 프래그먼트 생성
func (w *FMP4Writer) createTrackFragment(trackID uint32, frames []media.Frame) TrackFragment {
	traf := TrackFragment{
		TrackID:           trackID,
		BaseDataOffset:    0, // MOOF 박스 시작 기준
		SampleDescription: 1, // 첫 번째 샘플 설명 사용
		DefaultDuration:   0, // 개별 샘플마다 설정
		DefaultSize:       0, // 개별 샘플마다 설정
		DefaultFlags:      SampleFlagNonKeyFrame, // 기본값
		Samples:           make([]SampleInfo, 0, len(frames)),
	}
	
	// 각 프레임을 샘플로 변환
	for i, frame := range frames {
		sample := w.frameToSample(frame, trackID)
		
		// 첫 번째 비디오 프레임이 키프레임인지 확인
		if trackID == VideoTrackID && i == 0 && media.IsKeyFrame(frame.FrameType) {
			sample.Flags = SampleFlagKeyFrame
		}
		
		traf.Samples = append(traf.Samples, sample)
	}
	
	return traf
}

// frameToSample 프레임을 샘플로 변환
func (w *FMP4Writer) frameToSample(frame media.Frame, trackID uint32) SampleInfo {
	// 프레임 데이터 병합
	data := w.mergeFrameData(frame.Data)
	
	// 지속시간 계산 (타임스케일 기준)
	var duration uint32
	if trackID == VideoTrackID {
		// 비디오: 30fps 가정하면 90000/30 = 3000 틱
		duration = VideoTimeScale / 30
	} else {
		// 오디오: AAC 프레임은 보통 1024 샘플 = 1024/48000 * 48000 = 1024 틱
		duration = 1024
	}
	
	sample := SampleInfo{
		Duration:          duration,
		Size:             uint32(len(data)),
		Flags:            SampleFlagNonKeyFrame, // 기본값
		CompositionOffset: 0,                   // PTS-DTS 오프셋 (B-프레임 지원용)
		Data:             data,
	}
	
	return sample
}

// mergeFrameData 프레임 데이터 청크들을 병합
func (w *FMP4Writer) mergeFrameData(chunks [][]byte) []byte {
	totalSize := 0
	for _, chunk := range chunks {
		totalSize += len(chunk)
	}
	
	result := make([]byte, totalSize)
	offset := 0
	for _, chunk := range chunks {
		copy(result[offset:], chunk)
		offset += len(chunk)
	}
	
	return result
}

// writeFTYP FTYP 박스 작성
func (w *FMP4Writer) writeFTYP() error {
	// 호환 브랜드들
	compatibleBrands := GetCompatibleBrands()
	
	// 박스 크기 계산: 헤더(8) + major_brand(4) + minor_version(4) + compatible_brands(4*n)
	boxSize := uint32(8 + 4 + 4 + 4*len(compatibleBrands))
	
	// 헤더 작성
	if err := w.writeBoxHeader(boxSize, BoxTypeFTYP); err != nil {
		return err
	}
	
	// Major brand
	w.buffer.WriteString(BrandISOBM)
	
	// Minor version
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	// Compatible brands
	for _, brand := range compatibleBrands {
		w.buffer.WriteString(brand)
	}
	
	return nil
}

// writeMOOV MOOV 박스 작성
func (w *FMP4Writer) writeMOOV(hasVideo, hasAudio bool) error {
	moovStart := w.buffer.Len()
	
	// MOOV 헤더 (크기는 나중에 업데이트)
	if err := w.writeBoxHeader(0, BoxTypeMOOV); err != nil {
		return err
	}
	
	// MVHD 박스 작성
	if err := w.writeMVHD(); err != nil {
		return err
	}
	
	// 트랙 박스들 작성
	trackID := uint32(1)
	
	if hasVideo {
		if err := w.writeVideoTRAK(trackID); err != nil {
			return err
		}
		trackID++
	}
	
	if hasAudio {
		if err := w.writeAudioTRAK(trackID); err != nil {
			return err
		}
	}
	
	// MVEX 박스 작성 (Movie Extends - fragmented MP4용)
	if err := w.writeMVEX(hasVideo, hasAudio); err != nil {
		return err
	}
	
	// MOOV 박스 크기 업데이트
	moovEnd := w.buffer.Len()
	moovSize := uint32(moovEnd - moovStart)
	w.updateBoxSize(moovStart, moovSize)
	
	return nil
}

// writeBoxHeader 박스 헤더 작성
func (w *FMP4Writer) writeBoxHeader(size uint32, boxType string) error {
	binary.Write(w.buffer, binary.BigEndian, size)
	w.buffer.WriteString(boxType)
	return nil
}

// writeFullBoxHeader Full Box 헤더 작성
func (w *FMP4Writer) writeFullBoxHeader(size uint32, boxType string, version uint8, flags uint32) error {
	if err := w.writeBoxHeader(size, boxType); err != nil {
		return err
	}
	
	// Version (1바이트) + Flags (3바이트)
	versionFlags := uint32(version)<<24 | (flags & 0xFFFFFF)
	binary.Write(w.buffer, binary.BigEndian, versionFlags)
	
	return nil
}

// updateBoxSize 박스 크기 업데이트
func (w *FMP4Writer) updateBoxSize(offset int, size uint32) {
	data := w.buffer.Bytes()
	binary.BigEndian.PutUint32(data[offset:offset+4], size)
}

// writeMVHD MVHD 박스 작성
func (w *FMP4Writer) writeMVHD() error {
	boxSize := uint32(8 + 4 + 96) // 헤더 + version/flags + MVHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeMVHD, 0, 0); err != nil {
		return err
	}
	
	now := uint32(time.Now().Unix())
	
	// Creation time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Modification time  
	binary.Write(w.buffer, binary.BigEndian, now)
	// Timescale
	binary.Write(w.buffer, binary.BigEndian, w.timescale)
	// Duration (0 for live streams)
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	// Rate (1.0 = 0x00010000)
	binary.Write(w.buffer, binary.BigEndian, uint32(0x00010000))
	// Volume (1.0 = 0x0100)
	binary.Write(w.buffer, binary.BigEndian, uint16(0x0100))
	
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	binary.Write(w.buffer, binary.BigEndian, [2]uint32{0, 0})
	
	// Matrix (identity matrix)
	matrix := [9]uint32{
		0x00010000, 0, 0,
		0, 0x00010000, 0,
		0, 0, 0x40000000,
	}
	for _, val := range matrix {
		binary.Write(w.buffer, binary.BigEndian, val)
	}
	
	// Pre-defined
	for i := 0; i < 6; i++ {
		binary.Write(w.buffer, binary.BigEndian, uint32(0))
	}
	
	// Next track ID
	binary.Write(w.buffer, binary.BigEndian, uint32(3)) // 비디오(1) + 오디오(2) + 다음(3)
	
	return nil
}

// SetVideoTrack 비디오 트랙 정보 설정
func (w *FMP4Writer) SetVideoTrack(width, height uint16, codecConfig []byte) {
	w.videoTrack = &TrackInfo{
		ID:          VideoTrackID,
		Type:        "video",
		Timescale:   VideoTimeScale,
		Width:       width,
		Height:      height,
		CodecConfig: codecConfig,
	}
}

// SetAudioTrack 오디오 트랙 정보 설정
func (w *FMP4Writer) SetAudioTrack(sampleRate uint32, channels uint16, codecConfig []byte) {
	w.audioTrack = &TrackInfo{
		ID:          AudioTrackID,
		Type:        "audio",
		Timescale:   sampleRate, // 오디오는 샘플레이트를 타임스케일로 사용
		SampleRate:  sampleRate,
		Channels:    channels,
		CodecConfig: codecConfig,
	}
}

// writeMVEX MVEX 박스 작성 (Movie Extends)
func (w *FMP4Writer) writeMVEX(hasVideo, hasAudio bool) error {
	mvexStart := w.buffer.Len()
	
	// MVEX 헤더
	if err := w.writeBoxHeader(0, "mvex"); err != nil {
		return err
	}
	
	// MEHD 박스 (Movie Extends Header)
	if err := w.writeMEHD(); err != nil {
		return err
	}
	
	// TREX 박스들 (Track Extends)
	trackID := uint32(1)
	
	if hasVideo {
		if err := w.writeTREX(trackID, SampleFlagNonKeyFrame); err != nil {
			return err
		}
		trackID++
	}
	
	if hasAudio {
		if err := w.writeTREX(trackID, SampleFlagNonKeyFrame); err != nil {
			return err
		}
	}
	
	// MVEX 박스 크기 업데이트
	mvexEnd := w.buffer.Len()
	mvexSize := uint32(mvexEnd - mvexStart)
	w.updateBoxSize(mvexStart, mvexSize)
	
	return nil
}

// writeMEHD MEHD 박스 작성 (Movie Extends Header)
func (w *FMP4Writer) writeMEHD() error {
	boxSize := uint32(8 + 4 + 8) // 헤더 + version/flags + duration
	
	if err := w.writeFullBoxHeader(boxSize, "mehd", 1, 0); err != nil {
		return err
	}
	
	// Fragment duration (0 for live streams)
	binary.Write(w.buffer, binary.BigEndian, uint64(0))
	
	return nil
}

// writeTREX TREX 박스 작성 (Track Extends)
func (w *FMP4Writer) writeTREX(trackID, defaultFlags uint32) error {
	boxSize := uint32(8 + 4 + 20) // 헤더 + version/flags + TREX 데이터
	
	if err := w.writeFullBoxHeader(boxSize, "trex", 0, 0); err != nil {
		return err
	}
	
	// Track ID
	binary.Write(w.buffer, binary.BigEndian, trackID)
	// Default sample description index
	binary.Write(w.buffer, binary.BigEndian, uint32(1))
	// Default sample duration
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	// Default sample size
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	// Default sample flags
	binary.Write(w.buffer, binary.BigEndian, defaultFlags)
	
	return nil
}

// writeMOOF MOOF 박스 작성 (Movie Fragment)
func (w *FMP4Writer) writeMOOF(fragment *Fragment) error {
	moofStart := w.buffer.Len()
	
	// MOOF 헤더
	if err := w.writeBoxHeader(0, BoxTypeMOOF); err != nil {
		return err
	}
	
	// MFHD 박스 (Movie Fragment Header)
	if err := w.writeMFHD(fragment.SequenceNumber); err != nil {
		return err
	}
	
	// TRAF 박스들 (Track Fragment)
	for _, traf := range fragment.TrackFragments {
		if err := w.writeTRAF(&traf, moofStart); err != nil {
			return err
		}
	}
	
	// MOOF 박스 크기 업데이트
	moofEnd := w.buffer.Len()
	moofSize := uint32(moofEnd - moofStart)
	w.updateBoxSize(moofStart, moofSize)
	
	// 전체 프래그먼트 크기 설정
	fragment.TotalSize = moofSize
	
	return nil
}

// writeMFHD MFHD 박스 작성 (Movie Fragment Header)
func (w *FMP4Writer) writeMFHD(sequenceNumber uint32) error {
	boxSize := uint32(8 + 4 + 4) // 헤더 + version/flags + sequence_number
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeMFHD, 0, 0); err != nil {
		return err
	}
	
	// Sequence number
	binary.Write(w.buffer, binary.BigEndian, sequenceNumber)
	
	return nil
}

// writeTRAF TRAF 박스 작성 (Track Fragment)
func (w *FMP4Writer) writeTRAF(traf *TrackFragment, moofStart int) error {
	trafStart := w.buffer.Len()
	
	// TRAF 헤더
	if err := w.writeBoxHeader(0, BoxTypeTRAF); err != nil {
		return err
	}
	
	// TFHD 박스 (Track Fragment Header)
	if err := w.writeTFHD(traf, uint32(moofStart)); err != nil {
		return err
	}
	
	// TRUN 박스 (Track Fragment Run)
	if err := w.writeTRUN(traf, uint32(w.buffer.Len()-moofStart)); err != nil {
		return err
	}
	
	// TRAF 박스 크기 업데이트
	trafEnd := w.buffer.Len()
	trafSize := uint32(trafEnd - trafStart)
	w.updateBoxSize(trafStart, trafSize)
	
	return nil
}

// writeTFHD TFHD 박스 작성 (Track Fragment Header)
func (w *FMP4Writer) writeTFHD(traf *TrackFragment, baseDataOffset uint32) error {
	// 플래그 설정
	flags := TFHDBaseDataOffset
	if traf.DefaultDuration > 0 {
		flags |= TFHDDefaultSampleDuration
	}
	if traf.DefaultSize > 0 {
		flags |= TFHDDefaultSampleSize
	}
	if traf.DefaultFlags > 0 {
		flags |= TFHDDefaultSampleFlags
	}
	
	// 박스 크기 계산
	boxSize := uint32(8 + 4 + 4 + 8) // 헤더 + version/flags + track_id + base_data_offset
	if flags&TFHDDefaultSampleDuration != 0 {
		boxSize += 4
	}
	if flags&TFHDDefaultSampleSize != 0 {
		boxSize += 4
	}
	if flags&TFHDDefaultSampleFlags != 0 {
		boxSize += 4
	}
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeTFHD, 0, flags); err != nil {
		return err
	}
	
	// Track ID
	binary.Write(w.buffer, binary.BigEndian, traf.TrackID)
	// Base data offset
	binary.Write(w.buffer, binary.BigEndian, uint64(baseDataOffset))
	
	// 옵션 필드들
	if flags&TFHDDefaultSampleDuration != 0 {
		binary.Write(w.buffer, binary.BigEndian, traf.DefaultDuration)
	}
	if flags&TFHDDefaultSampleSize != 0 {
		binary.Write(w.buffer, binary.BigEndian, traf.DefaultSize)
	}
	if flags&TFHDDefaultSampleFlags != 0 {
		binary.Write(w.buffer, binary.BigEndian, traf.DefaultFlags)
	}
	
	return nil
}

// writeTRUN TRUN 박스 작성 (Track Fragment Run)
func (w *FMP4Writer) writeTRUN(traf *TrackFragment, dataOffset uint32) error {
	flags := TRUNDataOffset | TRUNSampleDuration | TRUNSampleSize | TRUNSampleFlags
	
	// 박스 크기 계산
	boxSize := uint32(8 + 4 + 4 + 4 + len(traf.Samples)*12) // 기본 + 샘플별 12바이트
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeTRUN, 0, flags); err != nil {
		return err
	}
	
	// Sample count
	binary.Write(w.buffer, binary.BigEndian, uint32(len(traf.Samples)))
	
	// Data offset (MDAT 박스 시작까지의 오프셋)
	mdat_offset := dataOffset + boxSize
	binary.Write(w.buffer, binary.BigEndian, mdat_offset)
	
	// 샘플 정보들
	for _, sample := range traf.Samples {
		binary.Write(w.buffer, binary.BigEndian, sample.Duration)
		binary.Write(w.buffer, binary.BigEndian, sample.Size)
		binary.Write(w.buffer, binary.BigEndian, sample.Flags)
	}
	
	return nil
}

// writeMDAT MDAT 박스 작성 (Media Data)
func (w *FMP4Writer) writeMDAT(fragment *Fragment) error {
	// 모든 샘플 데이터 크기 계산
	totalDataSize := 0
	for _, traf := range fragment.TrackFragments {
		for _, sample := range traf.Samples {
			totalDataSize += len(sample.Data)
		}
	}
	
	// MDAT 헤더
	mdatSize := uint32(8 + totalDataSize) // 헤더 + 데이터
	if err := w.writeBoxHeader(mdatSize, BoxTypeMDAT); err != nil {
		return err
	}
	
	// 샘플 데이터들 작성
	for _, traf := range fragment.TrackFragments {
		for _, sample := range traf.Samples {
			w.buffer.Write(sample.Data)
		}
	}
	
	fragment.TotalSize += mdatSize
	
	return nil
}