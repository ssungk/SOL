package media

import "errors"

// Common errors
var (
	ErrStreamNotFound   = errors.New("stream not found")
	ErrSessionNotActive = errors.New("session is not active")
	ErrNilSession       = errors.New("session cannot be nil")
)