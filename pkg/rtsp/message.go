package rtsp

import (
	"fmt"
	"strconv"
	"strings"
)

// Request represents an RTSP request
type Request struct {
	Method  string
	URI     string
	Version string
	Headers map[string]string
	Body    []byte
	CSeq    int
}

// Response represents an RTSP response
type Response struct {
	Version    string
	StatusCode int
	StatusText string
	Headers    map[string]string
	Body       []byte
	CSeq       int
}

// NewRequest creates a new RTSP request
func NewRequest(method, uri string) *Request {
	return &Request{
		Method:  method,
		URI:     uri,
		Version: RTSPVersion,
		Headers: make(map[string]string),
	}
}

// NewResponse creates a new RTSP response
func NewResponse(statusCode int) *Response {
	return &Response{
		Version:    RTSPVersion,
		StatusCode: statusCode,
		StatusText: getStatusText(statusCode),
		Headers:    make(map[string]string),
	}
}

// SetHeader sets a header value
func (r *Request) SetHeader(key, value string) {
	r.Headers[key] = value
}

// GetHeader gets a header value
func (r *Request) GetHeader(key string) string {
	return r.Headers[key]
}

// SetCSeq sets the CSeq header and field
func (r *Request) SetCSeq(cseq int) {
	r.CSeq = cseq
	r.Headers[HeaderCSeq] = strconv.Itoa(cseq)
}

// SetHeader sets a header value
func (r *Response) SetHeader(key, value string) {
	r.Headers[key] = value
}

// GetHeader gets a header value
func (r *Response) GetHeader(key string) string {
	return r.Headers[key]
}

// SetCSeq sets the CSeq header and field
func (r *Response) SetCSeq(cseq int) {
	r.CSeq = cseq
	r.Headers[HeaderCSeq] = strconv.Itoa(cseq)
}

// String returns the string representation of the request
func (r *Request) String() string {
	var sb strings.Builder
	
	// Request line
	sb.WriteString(fmt.Sprintf("%s %s %s\r\n", r.Method, r.URI, r.Version))
	
	// Headers
	for key, value := range r.Headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	
	// Empty line
	sb.WriteString("\r\n")
	
	// Body
	if len(r.Body) > 0 {
		sb.Write(r.Body)
	}
	
	return sb.String()
}

// String returns the string representation of the response
func (r *Response) String() string {
	var sb strings.Builder
	
	// Status line
	sb.WriteString(fmt.Sprintf("%s %d %s\r\n", r.Version, r.StatusCode, r.StatusText))
	
	// Headers
	for key, value := range r.Headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	
	// Empty line
	sb.WriteString("\r\n")
	
	// Body
	if len(r.Body) > 0 {
		sb.Write(r.Body)
	}
	
	return sb.String()
}

// Bytes returns the byte representation of the request
func (r *Request) Bytes() []byte {
	return []byte(r.String())
}

// Bytes returns the byte representation of the response
func (r *Response) Bytes() []byte {
	return []byte(r.String())
}

// getStatusText returns the standard status text for a status code
func getStatusText(statusCode int) string {
	switch statusCode {
	case StatusOK:
		return "OK"
	case StatusCreated:
		return "Created"
	case StatusLowOnStorageSpace:
		return "Low on Storage Space"
	case StatusMultipleChoices:
		return "Multiple Choices"
	case StatusMovedPermanently:
		return "Moved Permanently"
	case StatusMovedTemporarily:
		return "Moved Temporarily"
	case StatusSeeOther:
		return "See Other"
	case StatusNotModified:
		return "Not Modified"
	case StatusUseProxy:
		return "Use Proxy"
	case StatusBadRequest:
		return "Bad Request"
	case StatusUnauthorized:
		return "Unauthorized"
	case StatusPaymentRequired:
		return "Payment Required"
	case StatusForbidden:
		return "Forbidden"
	case StatusNotFound:
		return "Not Found"
	case StatusMethodNotAllowed:
		return "Method Not Allowed"
	case StatusNotAcceptable:
		return "Not Acceptable"
	case StatusProxyAuthRequired:
		return "Proxy Authentication Required"
	case StatusRequestTimeout:
		return "Request Time-out"
	case StatusGone:
		return "Gone"
	case StatusLengthRequired:
		return "Length Required"
	case StatusPreconditionFailed:
		return "Precondition Failed"
	case StatusRequestEntityTooLarge:
		return "Request Entity Too Large"
	case StatusRequestURITooLarge:
		return "Request-URI Too Large"
	case StatusUnsupportedMediaType:
		return "Unsupported Media Type"
	case StatusParameterNotUnderstood:
		return "Parameter Not Understood"
	case StatusConferenceNotFound:
		return "Conference Not Found"
	case StatusNotEnoughBandwidth:
		return "Not Enough Bandwidth"
	case StatusSessionNotFound:
		return "Session Not Found"
	case StatusMethodNotValidInThisState:
		return "Method Not Valid in This State"
	case StatusHeaderFieldNotValidForResource:
		return "Header Field Not Valid for Resource"
	case StatusInvalidRange:
		return "Invalid Range"
	case StatusParameterIsReadOnly:
		return "Parameter Is Read-Only"
	case StatusAggregateOperationNotAllowed:
		return "Aggregate operation not allowed"
	case StatusOnlyAggregateOperationAllowed:
		return "Only aggregate operation allowed"
	case StatusUnsupportedTransport:
		return "Unsupported transport"
	case StatusDestinationUnreachable:
		return "Destination unreachable"
	case StatusInternalServerError:
		return "Internal Server Error"
	case StatusNotImplemented:
		return "Not Implemented"
	case StatusBadGateway:
		return "Bad Gateway"
	case StatusServiceUnavailable:
		return "Service Unavailable"
	case StatusGatewayTimeout:
		return "Gateway Time-out"
	case StatusRTSPVersionNotSupported:
		return "RTSP Version not supported"
	case StatusOptionNotSupported:
		return "Option not supported"
	default:
		return "Unknown"
	}
}
