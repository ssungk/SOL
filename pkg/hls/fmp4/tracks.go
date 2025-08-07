package fmp4

import (
	"encoding/binary"
)

// writeVideoTRAK 비디오 트랙 박스 작성
func (w *FMP4Writer) writeVideoTRAK(trackID uint32) error {
	trakStart := w.buffer.Len()
	
	// TRAK 헤더
	if err := w.writeBoxHeader(0, BoxTypeTRAK); err != nil {
		return err
	}
	
	// TKHD 박스
	if err := w.writeVideoTKHD(trackID); err != nil {
		return err
	}
	
	// MDIA 박스
	if err := w.writeVideoMDIA(trackID); err != nil {
		return err
	}
	
	// TRAK 박스 크기 업데이트
	trakEnd := w.buffer.Len()
	trakSize := uint32(trakEnd - trakStart)
	w.updateBoxSize(trakStart, trakSize)
	
	return nil
}

// writeVideoTKHD 비디오 트랙 헤더 작성
func (w *FMP4Writer) writeVideoTKHD(trackID uint32) error {
	boxSize := uint32(8 + 4 + 84) // 헤더 + version/flags + TKHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeTKHD, 0, 0x000007); err != nil { // track_enabled | track_in_movie | track_in_preview
		return err
	}
	
	now := uint32(0) // 간단히 0으로 설정
	
	// Creation time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Modification time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Track ID
	binary.Write(w.buffer, binary.BigEndian, trackID)
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	// Duration
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, [2]uint32{0, 0})
	// Layer
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Alternate group
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Volume (비디오는 0)
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	
	// Matrix (identity matrix)
	matrix := [9]uint32{
		0x00010000, 0, 0,
		0, 0x00010000, 0,
		0, 0, 0x40000000,
	}
	for _, val := range matrix {
		binary.Write(w.buffer, binary.BigEndian, val)
	}
	
	// Width and height (16.16 고정소수점)
	width := uint32(1280) << 16  // 기본값 1280
	height := uint32(720) << 16  // 기본값 720
	
	if w.videoTrack != nil {
		width = uint32(w.videoTrack.Width) << 16
		height = uint32(w.videoTrack.Height) << 16
	}
	
	binary.Write(w.buffer, binary.BigEndian, width)
	binary.Write(w.buffer, binary.BigEndian, height)
	
	return nil
}

// writeVideoMDIA 비디오 미디어 박스 작성
func (w *FMP4Writer) writeVideoMDIA(trackID uint32) error {
	mdiaStart := w.buffer.Len()
	
	// MDIA 헤더
	if err := w.writeBoxHeader(0, BoxTypeMDIA); err != nil {
		return err
	}
	
	// MDHD 박스
	if err := w.writeVideoMDHD(); err != nil {
		return err
	}
	
	// HDLR 박스
	if err := w.writeVideoHDLR(); err != nil {
		return err
	}
	
	// MINF 박스
	if err := w.writeVideoMINF(); err != nil {
		return err
	}
	
	// MDIA 박스 크기 업데이트
	mdiaEnd := w.buffer.Len()
	mdiaSize := uint32(mdiaEnd - mdiaStart)
	w.updateBoxSize(mdiaStart, mdiaSize)
	
	return nil
}

// writeVideoMDHD 비디오 미디어 헤더 작성
func (w *FMP4Writer) writeVideoMDHD() error {
	boxSize := uint32(8 + 4 + 20) // 헤더 + version/flags + MDHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeMDHD, 0, 0); err != nil {
		return err
	}
	
	now := uint32(0)
	timescale := VideoTimeScale
	if w.videoTrack != nil {
		timescale = w.videoTrack.Timescale
	}
	
	// Creation time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Modification time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Timescale
	binary.Write(w.buffer, binary.BigEndian, timescale)
	// Duration
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	// Language (undetermined = 0x55C4)
	binary.Write(w.buffer, binary.BigEndian, uint16(0x55C4))
	// Pre-defined
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	
	return nil
}

// writeVideoHDLR 비디오 핸들러 박스 작성
func (w *FMP4Writer) writeVideoHDLR() error {
	handlerName := "VideoHandler"
	boxSize := uint32(8 + 4 + 20 + len(handlerName) + 1) // +1 for null terminator
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeHDLR, 0, 0); err != nil {
		return err
	}
	
	// Pre-defined
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	// Handler type
	w.buffer.WriteString(HandlerVideo)
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, [3]uint32{0, 0, 0})
	// Name
	w.buffer.WriteString(handlerName)
	w.buffer.WriteByte(0) // null terminator
	
	return nil
}

// writeVideoMINF 비디오 미디어 정보 박스 작성
func (w *FMP4Writer) writeVideoMINF() error {
	minfStart := w.buffer.Len()
	
	// MINF 헤더
	if err := w.writeBoxHeader(0, BoxTypeMINF); err != nil {
		return err
	}
	
	// VMHD 박스
	if err := w.writeVMHD(); err != nil {
		return err
	}
	
	// DINF 박스
	if err := w.writeDINF(); err != nil {
		return err
	}
	
	// STBL 박스 (비어있는 샘플 테이블)
	if err := w.writeEmptySTBL(); err != nil {
		return err
	}
	
	// MINF 박스 크기 업데이트
	minfEnd := w.buffer.Len()
	minfSize := uint32(minfEnd - minfStart)
	w.updateBoxSize(minfStart, minfSize)
	
	return nil
}

// writeVMHD 비디오 미디어 헤더 박스 작성
func (w *FMP4Writer) writeVMHD() error {
	boxSize := uint32(8 + 4 + 8) // 헤더 + version/flags + VMHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeVMHD, 0, 1); err != nil { // flags = 1
		return err
	}
	
	// Graphics mode
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Opcolor (R, G, B)
	binary.Write(w.buffer, binary.BigEndian, [3]uint16{0, 0, 0})
	
	return nil
}

// writeAudioTRAK 오디오 트랙 박스 작성
func (w *FMP4Writer) writeAudioTRAK(trackID uint32) error {
	trakStart := w.buffer.Len()
	
	// TRAK 헤더
	if err := w.writeBoxHeader(0, BoxTypeTRAK); err != nil {
		return err
	}
	
	// TKHD 박스
	if err := w.writeAudioTKHD(trackID); err != nil {
		return err
	}
	
	// MDIA 박스
	if err := w.writeAudioMDIA(trackID); err != nil {
		return err
	}
	
	// TRAK 박스 크기 업데이트
	trakEnd := w.buffer.Len()
	trakSize := uint32(trakEnd - trakStart)
	w.updateBoxSize(trakStart, trakSize)
	
	return nil
}

// writeAudioTKHD 오디오 트랙 헤더 작성
func (w *FMP4Writer) writeAudioTKHD(trackID uint32) error {
	boxSize := uint32(8 + 4 + 84) // 헤더 + version/flags + TKHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeTKHD, 0, 0x000007); err != nil {
		return err
	}
	
	now := uint32(0)
	
	// Creation time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Modification time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Track ID
	binary.Write(w.buffer, binary.BigEndian, trackID)
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	// Duration
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, [2]uint32{0, 0})
	// Layer
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Alternate group
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Volume (오디오는 1.0 = 0x0100)
	binary.Write(w.buffer, binary.BigEndian, uint16(0x0100))
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	
	// Matrix (identity matrix)
	matrix := [9]uint32{
		0x00010000, 0, 0,
		0, 0x00010000, 0,
		0, 0, 0x40000000,
	}
	for _, val := range matrix {
		binary.Write(w.buffer, binary.BigEndian, val)
	}
	
	// Width and height (오디오는 0)
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	return nil
}

// writeAudioMDIA 오디오 미디어 박스 작성
func (w *FMP4Writer) writeAudioMDIA(trackID uint32) error {
	mdiaStart := w.buffer.Len()
	
	// MDIA 헤더
	if err := w.writeBoxHeader(0, BoxTypeMDIA); err != nil {
		return err
	}
	
	// MDHD 박스
	if err := w.writeAudioMDHD(); err != nil {
		return err
	}
	
	// HDLR 박스
	if err := w.writeAudioHDLR(); err != nil {
		return err
	}
	
	// MINF 박스
	if err := w.writeAudioMINF(); err != nil {
		return err
	}
	
	// MDIA 박스 크기 업데이트
	mdiaEnd := w.buffer.Len()
	mdiaSize := uint32(mdiaEnd - mdiaStart)
	w.updateBoxSize(mdiaStart, mdiaSize)
	
	return nil
}

// writeAudioMDHD 오디오 미디어 헤더 작성
func (w *FMP4Writer) writeAudioMDHD() error {
	boxSize := uint32(8 + 4 + 20) // 헤더 + version/flags + MDHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeMDHD, 0, 0); err != nil {
		return err
	}
	
	now := uint32(0)
	timescale := uint32(AudioTimeScale)
	if w.audioTrack != nil {
		timescale = w.audioTrack.Timescale
	}
	
	// Creation time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Modification time
	binary.Write(w.buffer, binary.BigEndian, now)
	// Timescale
	binary.Write(w.buffer, binary.BigEndian, timescale)
	// Duration
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	
	// Language (undetermined = 0x55C4)
	binary.Write(w.buffer, binary.BigEndian, uint16(0x55C4))
	// Pre-defined
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	
	return nil
}

// writeAudioHDLR 오디오 핸들러 박스 작성
func (w *FMP4Writer) writeAudioHDLR() error {
	handlerName := "SoundHandler"
	boxSize := uint32(8 + 4 + 20 + len(handlerName) + 1)
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeHDLR, 0, 0); err != nil {
		return err
	}
	
	// Pre-defined
	binary.Write(w.buffer, binary.BigEndian, uint32(0))
	// Handler type
	w.buffer.WriteString(HandlerAudio)
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, [3]uint32{0, 0, 0})
	// Name
	w.buffer.WriteString(handlerName)
	w.buffer.WriteByte(0) // null terminator
	
	return nil
}

// writeAudioMINF 오디오 미디어 정보 박스 작성
func (w *FMP4Writer) writeAudioMINF() error {
	minfStart := w.buffer.Len()
	
	// MINF 헤더
	if err := w.writeBoxHeader(0, BoxTypeMINF); err != nil {
		return err
	}
	
	// SMHD 박스
	if err := w.writeSMHD(); err != nil {
		return err
	}
	
	// DINF 박스
	if err := w.writeDINF(); err != nil {
		return err
	}
	
	// STBL 박스 (비어있는 샘플 테이블)
	if err := w.writeEmptySTBL(); err != nil {
		return err
	}
	
	// MINF 박스 크기 업데이트
	minfEnd := w.buffer.Len()
	minfSize := uint32(minfEnd - minfStart)
	w.updateBoxSize(minfStart, minfSize)
	
	return nil
}

// writeSMHD 사운드 미디어 헤더 박스 작성
func (w *FMP4Writer) writeSMHD() error {
	boxSize := uint32(8 + 4 + 4) // 헤더 + version/flags + SMHD 데이터
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeSMHD, 0, 0); err != nil {
		return err
	}
	
	// Balance (0.0 = center)
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	// Reserved
	binary.Write(w.buffer, binary.BigEndian, uint16(0))
	
	return nil
}

// writeDINF 데이터 정보 박스 작성
func (w *FMP4Writer) writeDINF() error {
	dinfStart := w.buffer.Len()
	
	// DINF 헤더
	if err := w.writeBoxHeader(0, BoxTypeDINF); err != nil {
		return err
	}
	
	// DREF 박스
	if err := w.writeDREF(); err != nil {
		return err
	}
	
	// DINF 박스 크기 업데이트
	dinfEnd := w.buffer.Len()
	dinfSize := uint32(dinfEnd - dinfStart)
	w.updateBoxSize(dinfStart, dinfSize)
	
	return nil
}

// writeDREF 데이터 참조 박스 작성
func (w *FMP4Writer) writeDREF() error {
	boxSize := uint32(8 + 4 + 4 + 12) // 헤더 + version/flags + entry_count + url entry
	
	if err := w.writeFullBoxHeader(boxSize, BoxTypeDREF, 0, 0); err != nil {
		return err
	}
	
	// Entry count
	binary.Write(w.buffer, binary.BigEndian, uint32(1))
	
	// URL entry (self-reference)
	if err := w.writeFullBoxHeader(12, "url ", 0, 0x000001); err != nil { // flags = 1 (media in same file)
		return err
	}
	
	return nil
}

// writeEmptySTBL 비어있는 샘플 테이블 박스 작성
func (w *FMP4Writer) writeEmptySTBL() error {
	stblStart := w.buffer.Len()
	
	// STBL 헤더
	if err := w.writeBoxHeader(0, BoxTypeSTBL); err != nil {
		return err
	}
	
	// 비어있는 STSD
	if err := w.writeFullBoxHeader(16, BoxTypeSTSD, 0, 0); err != nil {
		return err
	}
	binary.Write(w.buffer, binary.BigEndian, uint32(0)) // entry_count = 0
	
	// 비어있는 STTS
	if err := w.writeFullBoxHeader(16, BoxTypeSTTS, 0, 0); err != nil {
		return err
	}
	binary.Write(w.buffer, binary.BigEndian, uint32(0)) // entry_count = 0
	
	// 비어있는 STSC
	if err := w.writeFullBoxHeader(16, BoxTypeSTSC, 0, 0); err != nil {
		return err
	}
	binary.Write(w.buffer, binary.BigEndian, uint32(0)) // entry_count = 0
	
	// 비어있는 STSZ
	if err := w.writeFullBoxHeader(20, BoxTypeSTSZ, 0, 0); err != nil {
		return err
	}
	binary.Write(w.buffer, binary.BigEndian, uint32(0)) // sample_size = 0
	binary.Write(w.buffer, binary.BigEndian, uint32(0)) // sample_count = 0
	
	// 비어있는 STCO
	if err := w.writeFullBoxHeader(16, BoxTypeSTCO, 0, 0); err != nil {
		return err
	}
	binary.Write(w.buffer, binary.BigEndian, uint32(0)) // entry_count = 0
	
	// STBL 박스 크기 업데이트
	stblEnd := w.buffer.Len()
	stblSize := uint32(stblEnd - stblStart)
	w.updateBoxSize(stblStart, stblSize)
	
	return nil
}