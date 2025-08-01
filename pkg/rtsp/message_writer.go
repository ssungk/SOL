package rtsp

import (
	"bufio"
	"io"
)

// MessageWriter handles RTSP message writing
type MessageWriter struct {
	writer *bufio.Writer
}

// NewMessageWriter creates a new RTSP message writer
func NewMessageWriter(w io.Writer) *MessageWriter {
	return &MessageWriter{
		writer: bufio.NewWriter(w),
	}
}

// WriteRequest writes an RTSP request
func (mw *MessageWriter) WriteRequest(req *Request) error {
	data := req.Bytes()
	if _, err := mw.writer.Write(data); err != nil {
		return err
	}
	return mw.writer.Flush()
}

// WriteResponse writes an RTSP response
func (mw *MessageWriter) WriteResponse(resp *Response) error {
	data := resp.Bytes()
	if _, err := mw.writer.Write(data); err != nil {
		return err
	}
	return mw.writer.Flush()
}
