package transport

import (
	"bytes"
	"context"
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
	s := NewStreamSession(rwc)

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

	s := NewHTTPSession(func(body io.Reader) (*http.Response, error) {
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

	s := NewHTTPSession(func(io.Reader) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})

	require.NoError(t, s.Writer().Close())

	_, err := io.ReadAll(s.Reader())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestClient_Open(t *testing.T) {
	t.Parallel()

	called := false
	testTransport := &mockTransport{
		openFn: func(_ context.Context, req *Request) (Session, error) {
			called = true
			assert.Equal(t, "git-upload-pack", req.Command)
			return NewStreamSession(&pipeRWC{}), nil
		},
	}

	c := NewClient(
		WithoutBuiltins(),
		WithScheme("ssh", func(ClientOptions) Transport { return testTransport }),
	)
	defer c.Close()

	req := &Request{
		URL:      &url.URL{Scheme: "ssh", Host: "github.com", Path: "/foo/bar.git"},
		Command:  "git-upload-pack",
		Protocol: protocol.V2,
	}

	sess, err := c.Open(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, called)
	require.NoError(t, sess.Close())
}

func TestClient_UnsupportedScheme(t *testing.T) {
	t.Parallel()

	c := NewClient(WithoutBuiltins())
	defer c.Close()

	req := &Request{
		URL: &url.URL{Scheme: "ftp", Host: "example.com"},
	}

	_, err := c.Open(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scheme")
}

func TestClient_NilRequest(t *testing.T) {
	t.Parallel()

	c := NewClient(WithoutBuiltins())
	defer c.Close()

	_, err := c.Open(context.Background(), nil)
	require.Error(t, err)
}

func TestClient_CallOptionsOverride(t *testing.T) {
	t.Parallel()

	var capturedOpts ClientOptions
	c := NewClient(
		WithoutBuiltins(),
		WithScheme("ssh", func(opts ClientOptions) Transport {
			capturedOpts = opts
			return &mockTransport{
				openFn: func(context.Context, *Request) (Session, error) {
					return NewStreamSession(&pipeRWC{}), nil
				},
			}
		}),
		WithProxy(ProxyOptions{
			HTTPProxy: http.ProxyFromEnvironment,
		}),
	)
	defer c.Close()

	req := &Request{
		URL: &url.URL{Scheme: "ssh", Host: "github.com", Path: "/foo.git"},
	}

	overrideProxy := func(_ *http.Request) (*url.URL, error) {
		return url.Parse("socks5://proxy:1080")
	}

	sess, err := c.Open(context.Background(), req, WithCallProxy(ProxyOptions{
		HTTPProxy: overrideProxy,
	}))
	require.NoError(t, err)
	require.NoError(t, sess.Close())

	assert.NotNil(t, capturedOpts.Proxy.HTTPProxy)
	proxyURL, _ := capturedOpts.Proxy.HTTPProxy(nil)
	assert.Equal(t, "socks5://proxy:1080", proxyURL.String())
}

func TestClient_Transport(t *testing.T) {
	t.Parallel()

	testTransport := &mockTransport{}

	c := NewClient(
		WithoutBuiltins(),
		WithScheme("git", func(ClientOptions) Transport { return testTransport }),
	)
	defer c.Close()

	tr, err := c.Transport("git")
	require.NoError(t, err)
	assert.Equal(t, testTransport, tr)

	_, err = c.Transport("ftp")
	require.Error(t, err)
}

func TestResolveOptions(t *testing.T) {
	t.Parallel()

	defaults := ClientOptions{
		HTTP: HTTPOptions{
			Authorizer: func(*http.Request) error { return nil },
		},
	}

	overrides := &CallOptions{
		HTTP: &HTTPOptions{
			Authorizer: func(*http.Request) error { return errors.New("override") },
		},
	}

	resolved := resolveOptions(defaults, overrides)
	err := resolved.HTTP.Authorizer(nil)
	require.Error(t, err)
	assert.Equal(t, "override", err.Error())

	resolved2 := resolveOptions(defaults, nil)
	require.NoError(t, resolved2.HTTP.Authorizer(nil))
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

type mockTransport struct {
	openFn func(context.Context, *Request) (Session, error)
}

func (m *mockTransport) Open(ctx context.Context, req *Request) (Session, error) {
	if m.openFn != nil {
		return m.openFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}
