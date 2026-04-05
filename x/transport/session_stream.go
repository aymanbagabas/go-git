package transport

import "io"

// streamSession wraps an io.ReadWriteCloser into a Session.
// Used by SSH, Git TCP, and file transports where one underlying stream
// backs both the read and write sides.
type streamSession struct {
	rwc io.ReadWriteCloser
}

// NewStreamSession creates a Session backed by a full-duplex stream.
//
// Writer().Close() is a no-op on stream sessions — it does not close
// the underlying connection. Only Session.Close() closes the full
// connection. This matches the pack protocol's need to write requests
// and then read responses over the same stream.
func NewStreamSession(rwc io.ReadWriteCloser) Session {
	return &streamSession{rwc: rwc}
}

func (s *streamSession) Reader() io.Reader      { return s.rwc }
func (s *streamSession) Writer() io.WriteCloser { return nopWriteCloser{s.rwc} }
func (s *streamSession) Close() error           { return s.rwc.Close() }

// nopWriteCloser wraps a Writer with a no-op Close.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }
