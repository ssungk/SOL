package rtsp

// SessionTerminated represents session termination
type SessionTerminated struct {
	SessionId string
}

// DescribeRequested represents DESCRIBE request
type DescribeRequested struct {
	SessionId  string
	StreamPath string
}

// PlayStarted represents PLAY start
type PlayStarted struct {
	SessionId  string
	StreamPath string
}

// PlayStopped represents PLAY stop
type PlayStopped struct {
	SessionId  string
	StreamPath string
}

// RecordStarted represents RECORD start
type RecordStarted struct {
	SessionId  string
	StreamPath string
}

// RecordStopped represents RECORD stop
type RecordStopped struct {
	SessionId  string
	StreamPath string
}

// AnnounceReceived represents ANNOUNCE with SDP
type AnnounceReceived struct {
	SessionId  string
	StreamPath string
	SDP        string
}

// RTPPacketReceived represents RTP packet data from client
type RTPPacketReceived struct {
	SessionId   string
	StreamPath  string
	Data        []byte
	Timestamp   uint32
	PayloadType uint8
}
