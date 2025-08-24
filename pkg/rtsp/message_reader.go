package rtsp

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// MessageReader handles RTSP message parsing
type MessageReader struct {
	reader *bufio.Reader
}

// NewMessageReader creates a new RTSP message reader
func NewMessageReader(r io.Reader) *MessageReader {
	return &MessageReader{
		reader: bufio.NewReaderSize(r, 8192), // 8KB 버퍼 크기 (RTMP와 동일)
	}
}

// ReadRequest reads and parses an RTSP request
func (mr *MessageReader) ReadRequest() (*Request, error) {
	// Read request line
	line, err := mr.readLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read request line: %w", err)
	}
	
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid request line: %s", line)
	}
	
	request := &Request{
		Method:  parts[0],
		URI:     parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}
	
	// Read headers
	if err := mr.readHeaders(request.Headers); err != nil {
		return nil, fmt.Errorf("failed to read headers: %w", err)
	}
	
	// Parse CSeq
	if cseqStr := request.Headers[HeaderCSeq]; cseqStr != "" {
		if cseq, err := strconv.Atoi(cseqStr); err == nil {
			request.CSeq = cseq
		}
	}
	
	// Read body if Content-Length is specified
	if contentLengthStr := request.Headers[HeaderContentLength]; contentLengthStr != "" {
		contentLength, err := strconv.Atoi(contentLengthStr)
		if err != nil {
			return nil, fmt.Errorf("invalid content length: %s", contentLengthStr)
		}
		
		if contentLength > 0 {
			request.Body = make([]byte, contentLength)
			if _, err := io.ReadFull(mr.reader, request.Body); err != nil {
				return nil, fmt.Errorf("failed to read body: %w", err)
			}
		}
	}
	
	return request, nil
}

// ReadResponse reads and parses an RTSP response
func (mr *MessageReader) ReadResponse() (*Response, error) {
	// Read status line
	line, err := mr.readLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read status line: %w", err)
	}
	
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid status line: %s", line)
	}
	
	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %s", parts[1])
	}
	
	statusText := ""
	if len(parts) == 3 {
		statusText = parts[2]
	}
	
	response := &Response{
		Version:    parts[0],
		StatusCode: statusCode,
		StatusText: statusText,
		Headers:    make(map[string]string),
	}
	
	// Read headers
	if err := mr.readHeaders(response.Headers); err != nil {
		return nil, fmt.Errorf("failed to read headers: %w", err)
	}
	
	// Parse CSeq
	if cseqStr := response.Headers[HeaderCSeq]; cseqStr != "" {
		if cseq, err := strconv.Atoi(cseqStr); err == nil {
			response.CSeq = cseq
		}
	}
	
	// Read body if Content-Length is specified
	if contentLengthStr := response.Headers[HeaderContentLength]; contentLengthStr != "" {
		contentLength, err := strconv.Atoi(contentLengthStr)
		if err != nil {
			return nil, fmt.Errorf("invalid content length: %s", contentLengthStr)
		}
		
		if contentLength > 0 {
			response.Body = make([]byte, contentLength)
			if _, err := io.ReadFull(mr.reader, response.Body); err != nil {
				return nil, fmt.Errorf("failed to read body: %w", err)
			}
		}
	}
	
	return response, nil
}

// readLine reads a line from the reader (removes \r\n)
func (mr *MessageReader) readLine() (string, error) {
	line, err := mr.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	
	// Remove \r\n
	line = strings.TrimRight(line, "\r\n")
	return line, nil
}

// readHeaders reads headers until an empty line
func (mr *MessageReader) readHeaders(headers map[string]string) error {
	for {
		line, err := mr.readLine()
		if err != nil {
			return err
		}
		
		// Empty line means end of headers
		if line == "" {
			break
		}
		
		// Parse header
		colonIndex := strings.Index(line, ":")
		if colonIndex == -1 {
			continue // Skip invalid header lines
		}
		
		key := strings.TrimSpace(line[:colonIndex])
		value := strings.TrimSpace(line[colonIndex+1:])
		headers[key] = value
	}
	
	return nil
}
