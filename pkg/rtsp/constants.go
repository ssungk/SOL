package rtsp

// RTSP Methods
const (
	MethodOptions    = "OPTIONS"
	MethodDescribe   = "DESCRIBE"
	MethodSetup      = "SETUP"
	MethodPlay       = "PLAY"
	MethodPause      = "PAUSE"
	MethodTeardown   = "TEARDOWN"
	MethodGetParam   = "GET_PARAMETER"
	MethodSetParam   = "SET_PARAMETER"
	MethodRecord     = "RECORD"
	MethodAnnounce   = "ANNOUNCE"
)

// RTSP Status Codes
const (
	StatusOK                    = 200
	StatusCreated              = 201
	StatusLowOnStorageSpace    = 250
	StatusMultipleChoices      = 300
	StatusMovedPermanently     = 301
	StatusMovedTemporarily     = 302
	StatusSeeOther             = 303
	StatusNotModified          = 304
	StatusUseProxy             = 305
	StatusBadRequest           = 400
	StatusUnauthorized         = 401
	StatusPaymentRequired      = 402
	StatusForbidden            = 403
	StatusNotFound             = 404
	StatusMethodNotAllowed     = 405
	StatusNotAcceptable        = 406
	StatusProxyAuthRequired    = 407
	StatusRequestTimeout       = 408
	StatusGone                 = 410
	StatusLengthRequired       = 411
	StatusPreconditionFailed   = 412
	StatusRequestEntityTooLarge = 413
	StatusRequestURITooLarge   = 414
	StatusUnsupportedMediaType = 415
	StatusParameterNotUnderstood = 451
	StatusConferenceNotFound   = 452
	StatusNotEnoughBandwidth   = 453
	StatusSessionNotFound      = 454
	StatusMethodNotValidInThisState = 455
	StatusHeaderFieldNotValidForResource = 456
	StatusInvalidRange         = 457
	StatusParameterIsReadOnly  = 458
	StatusAggregateOperationNotAllowed = 459
	StatusOnlyAggregateOperationAllowed = 460
	StatusUnsupportedTransport = 461
	StatusDestinationUnreachable = 462
	StatusInternalServerError  = 500
	StatusNotImplemented       = 501
	StatusBadGateway           = 502
	StatusServiceUnavailable   = 503
	StatusGatewayTimeout       = 504
	StatusRTSPVersionNotSupported = 505
	StatusOptionNotSupported   = 551
)

// RTSP Headers
const (
	HeaderAccept          = "Accept"
	HeaderAllow           = "Allow"
	HeaderAuthorization   = "Authorization"
	HeaderBandwidth       = "Bandwidth"
	HeaderBlocksize       = "Blocksize"
	HeaderCacheControl    = "Cache-Control"
	HeaderConference      = "Conference"
	HeaderConnection      = "Connection"
	HeaderContentBase     = "Content-Base"
	HeaderContentEncoding = "Content-Encoding"
	HeaderContentLanguage = "Content-Language"
	HeaderContentLength   = "Content-Length"
	HeaderContentLocation = "Content-Location"
	HeaderContentType     = "Content-Type"
	HeaderCSeq            = "CSeq"
	HeaderDate            = "Date"
	HeaderExpires         = "Expires"
	HeaderFrom            = "From"
	HeaderIfModifiedSince = "If-Modified-Since"
	HeaderLastModified    = "Last-Modified"
	HeaderProxyAuthenticate = "Proxy-Authenticate"
	HeaderProxyRequire    = "Proxy-Require"
	HeaderPublic          = "Public"
	HeaderRange           = "Range"
	HeaderReferer         = "Referer"
	HeaderRequire         = "Require"
	HeaderRetryAfter      = "Retry-After"
	HeaderRTPInfo         = "RTP-Info"
	HeaderScale           = "Scale"
	HeaderSession         = "Session"
	HeaderServer          = "Server"
	HeaderSpeed           = "Speed"
	HeaderTransport       = "Transport"
	HeaderUnsupported     = "Unsupported"
	HeaderUserAgent       = "User-Agent"
	HeaderVary            = "Vary"
	HeaderVia             = "Via"
	HeaderWWWAuthenticate = "WWW-Authenticate"
)

// Transport Protocols
const (
	TransportRTPUDP  = "RTP/AVP"
	TransportRTPTCP  = "RTP/AVP/TCP"
	TransportUnicast = "unicast"
	TransportMulticast = "multicast"
)

// RTSP Version
const RTSPVersion = "RTSP/1.0"

// Default Values
const (
	DefaultRTSPPort = 554
	DefaultTimeout  = 60 // seconds
)
