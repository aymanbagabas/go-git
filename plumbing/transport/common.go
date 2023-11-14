// Package transport includes the implementation for different transport
// protocols.
//
// `Client` can be used to fetch and send packfiles to a git server.
// The `client` package provides higher level functions to instantiate the
// appropriate `Client` based on the repository URL.
//
// go-git supports HTTP and SSH (see `Protocols`), but you can also install
// your own protocols (see the `client` package).
//
// Each protocol has its own implementation of `Client`, but you should
// generally not use them directly, use `client.NewClient` instead.
package transport

import (
	"errors"
)

var (
	ErrRepositoryNotFound     = errors.New("repository not found")
	ErrEmptyRemoteRepository  = errors.New("remote repository is empty")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrAuthorizationFailed    = errors.New("authorization failed")
	ErrEmptyUploadPackRequest = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod      = errors.New("invalid auth method")
	ErrAlreadyConnected       = errors.New("session already established")
	ErrUnsupportedService     = errors.New("unsupported service")
)
