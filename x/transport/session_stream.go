package transport

import "io"

// streamSession wraps separate reader and writer into a Session.
// Used by SSH, Git TCP, and file transports where one underlying stream
// backs both the read and write sides.
type streamSession struct {
	r     io.Reader
	w     io.WriteCloser
	close func() error
}

// NewStreamSession creates a Session from a reader, writer, and close function.
// Writer().Close() closes the write half only (signaling EOF to the remote).
// Session.Close() closes the full connection.
func NewStreamSession(r io.Reader, w io.WriteCloser, close func() error) Session {
	return &streamSession{r: r, w: w, close: close}
}

func (s *streamSession) Reader() io.Reader      { return s.r }
func (s *streamSession) Writer() io.WriteCloser { return s.w }
func (s *streamSession) Close() error           { return s.close() }
