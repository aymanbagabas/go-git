package transfer

import "errors"

// Predefined errors for Git transfer operations.
// TODO: add comments to each error
var (
	ErrRepositoryNotFound     = errors.New("repository not found")
	ErrEmptyRemoteRepository  = errors.New("remote repository is empty")
	ErrNoChange               = errors.New("no change")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrAuthorizationFailed    = errors.New("authorization failed")
	ErrEmptyUploadPackRequest = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod      = errors.New("invalid auth method")
	ErrAlreadyConnected       = errors.New("session already established")
	ErrInvalidRequest         = errors.New("invalid request")
)

var (
	// ErrUnsupportedVersion is returned when the protocol version is not
	// supported.
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
	// ErrUnsupportedService is returned when the service is not supported.
	ErrUnsupportedService = errors.New("unsupported service")
	// ErrInvalidResponse is returned when the response is invalid.
	ErrInvalidResponse = errors.New("invalid response")
	// ErrTimeoutExceeded is returned when the timeout is exceeded.
	ErrTimeoutExceeded = errors.New("timeout exceeded")
	// ErrPackedObjectsNotSupported is returned when the server does not support
	// packed objects.
	ErrPackedObjectsNotSupported = errors.New("packed objects not supported")
)
