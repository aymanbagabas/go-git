package transport

import "errors"

// Transport capability and support errors.
var (
	ErrConnectUnsupported  = errors.New("transport does not support raw connections")
	ErrCommandUnsupported  = errors.New("command is not supported by transport")
	ErrProtocolUnsupported = errors.New("protocol version is not supported")
)
