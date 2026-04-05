package transport

import "io"

// streamSession wraps separate reader and writer into a Session.
// Used by SSH, Git TCP, and file transports where one underlying stream
// backs both the read and write sides.
type conn struct {
	r     io.Reader
	w     io.WriteCloser
	close func() error
}

// NewStreamSession creates a Session from a reader, writer, and close function.
// Writer().Close() closes the write half only (signaling EOF to the remote).
// Session.Close() closes the full connection.
func NewConn(r io.Reader, w io.WriteCloser, close func() error) Conn {
	return &conn{r: r, w: w, close: close}
}

func (s *conn) Reader() io.Reader      { return s.r }
func (s *conn) Writer() io.WriteCloser { return s.w }
func (s *conn) Close() error           { return s.close() }
