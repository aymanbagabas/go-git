package transport

import (
	"errors"
	"io"
	"net/http"
)

// httpConn models one HTTP request/response exchange as a Session.
//
// The round-trip goroutine starts eagerly at construction: doFunc reads
// from the pipe while the caller writes to Writer(). When the caller
// closes Writer(), doFunc sees EOF and completes the HTTP exchange.
// Reader() blocks until the round-trip finishes.
type httpConn struct {
	pw *io.PipeWriter

	resp     *http.Response
	respErr  error
	respDone chan struct{}
}

// NewHTTPSession creates a Session backed by a single HTTP round-trip.
//
// doFunc is called immediately in a goroutine with a reader connected to
// Writer(). It should consume the request body and perform the HTTP
// request. The response becomes available via Reader() after doFunc returns.
func NewHTTPConn(doFunc func(body io.Reader) (*http.Response, error)) Conn {
	pr, pw := io.Pipe()
	s := &httpConn{
		pw:       pw,
		respDone: make(chan struct{}),
	}

	go func() {
		defer close(s.respDone)
		s.resp, s.respErr = doFunc(pr)
	}()

	return s
}

func (s *httpConn) Writer() io.WriteCloser { return s.pw }

func (s *httpConn) Reader() io.Reader {
	<-s.respDone
	if s.resp == nil {
		return errReader{err: s.respErr}
	}
	return s.resp.Body
}

func (s *httpConn) Close() error {
	_ = s.pw.Close()
	<-s.respDone
	if s.resp != nil && s.resp.Body != nil {
		return s.resp.Body.Close()
	}
	return nil
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return 0, errors.New("http session: no response available")
}
