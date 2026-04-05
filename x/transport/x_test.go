package transport

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

func TestStreamSession(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	rwc := &pipeRWC{Reader: pr, Writer: pw}
	s := NewConn(pr, pw, rwc.Close)

	go func() {
		_, err := s.Writer().Write([]byte("hello"))
		assert.NoError(t, err)
	}()

	buf := make([]byte, 5)
	_, err := io.ReadFull(s.Reader(), buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf))

	require.NoError(t, s.Close())
	assert.True(t, rwc.closed)
}

func TestHTTPSession_WriteCloseRead(t *testing.T) {
	t.Parallel()

	s := NewHTTPConn(func(body io.Reader) (*http.Response, error) {
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(append([]byte("echo:"), data...))),
		}, nil
	})

	w := s.Writer()
	_, err := w.Write([]byte("request"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	resp, err := io.ReadAll(s.Reader())
	require.NoError(t, err)
	assert.Equal(t, "echo:request", string(resp))

	require.NoError(t, s.Close())
}

func TestHTTPSession_DoFuncError(t *testing.T) {
	t.Parallel()

	s := NewHTTPConn(func(io.Reader) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})

	require.NoError(t, s.Writer().Close())

	_, err := io.ReadAll(s.Reader())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestGitProtocolEnv(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", GitProtocolEnv(protocol.V0))
	assert.Equal(t, "version=1", GitProtocolEnv(protocol.V1))
	assert.Equal(t, "version=2", GitProtocolEnv(protocol.V2))
}

func TestRequest(t *testing.T) {
	t.Parallel()

	req := &Request{
		URL:      &url.URL{Scheme: "ssh", Host: "github.com", Path: "/foo/bar.git"},
		Command:  "git-upload-pack",
		Args:     []string{"download"},
		Protocol: protocol.V2,
	}

	assert.Equal(t, "ssh", req.URL.Scheme)
	assert.Equal(t, "/foo/bar.git", req.URL.Path)
	assert.Equal(t, "git-upload-pack", req.Command)
	assert.Equal(t, protocol.V2, req.Protocol)
}

// test helpers

type pipeRWC struct {
	io.Reader
	io.Writer
	closed bool
}

func (p *pipeRWC) Close() error {
	p.closed = true
	if c, ok := p.Reader.(io.Closer); ok {
		c.Close()
	}
	if c, ok := p.Writer.(io.Closer); ok {
		c.Close()
	}
	return nil
}
