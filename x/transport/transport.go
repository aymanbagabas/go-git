package transport

import (
	"context"
	"io"
)

// Session is the universal result of opening one transport exchange.
//
// A session is single-shot: it represents one opened exchange for one Request.
// It is transport-neutral: it does not expose Git pack methods, refs,
// capabilities, or LFS concepts.
//
// All Session implementations follow a write-then-close-writer-then-read
// lifecycle:
//
//  1. The caller writes the request payload to Writer().
//  2. The caller closes Writer() to signal that the request is complete.
//  3. The caller reads the response from Reader().
//  4. The caller calls Close() to release all resources.
//
// For stream-backed sessions (SSH, Git TCP, file), Reader() and Writer()
// refer to the same underlying io.ReadWriteCloser. Closing the writer
// signals the write half of the stream.
//
// For HTTP-backed sessions, Writer() returns a pipe that buffers the
// request body. The HTTP round-trip is triggered when the caller closes
// Writer(). Only after the round-trip completes does Reader() become
// readable.
//
// Adapters must not attempt to read from Reader() before closing Writer().
// Violating this contract may deadlock on HTTP-backed sessions.
type Conn interface {
	io.Closer
	Reader() io.Reader
	Writer() io.WriteCloser
}

// Transport is the universal transport interface. All built-in transports
// implement Transport.
//

// Connectable is an optional lower-level capability implemented only by
// transports that can truly open a raw full-duplex stream.
//
// This interface is expected to be implemented by SSH, Git TCP, file, and
// custom helper transports. It is explicitly not implemented by HTTP
// transports.
type Connectable interface {
	Connect(context.Context, *Request) (Conn, error)
}
