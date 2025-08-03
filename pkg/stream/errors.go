package stream

import "errors"

// Common errors
var (
	ErrStreamNotFound    = errors.New("stream not found")
	ErrSessionNotActive  = errors.New("session is not active")
	ErrInvalidMediaType  = errors.New("invalid media type")
	ErrNilSession        = errors.New("session cannot be nil")
)
