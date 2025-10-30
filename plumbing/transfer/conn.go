package transfer

import "io"

// Conn is a connection to a Git remote repository.
type Conn interface {
	io.ReadWriteCloser

	// IsStateless returns whether the connection is stateless or not.
	IsStateless() bool
}
