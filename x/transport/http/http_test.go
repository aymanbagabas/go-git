package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol"
	transport "github.com/go-git/go-git/v6/x/transport"
)

func TestHTTPTransport_Open(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte("echo:" + string(body)))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack"}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	w := sess.Writer()
	_, err = w.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	resp, err := io.ReadAll(sess.Reader())
	require.NoError(t, err)
	assert.Equal(t, "echo:hello", string(resp))
	require.NoError(t, sess.Close())
}

func TestHTTPTransport_Authorizer(t *testing.T) {
	t.Parallel()
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{
		Authorizer: func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer test-token")
			return nil
		},
	})
	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack"}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	_, _ = io.ReadAll(sess.Reader())
	require.NoError(t, sess.Close())
	assert.Equal(t, "Bearer test-token", capturedAuth)
}

func TestHTTPTransport_NotConnectable(t *testing.T) {
	t.Parallel()
	tr := NewTransport(Options{})
	_, ok := interface{}(tr).(transport.Connectable)
	assert.False(t, ok)
}

func TestHTTPTransport_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack"}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	body, err := io.ReadAll(sess.Reader())
	require.NoError(t, err)
	assert.Equal(t, "internal error", string(body))
	require.NoError(t, sess.Close())
}

func TestHTTPTransport_GitProtocolV2(t *testing.T) {
	t.Parallel()
	var capturedProtocol string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedProtocol = r.Header.Get("Git-Protocol")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack", Protocol: protocol.V2}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	_, _ = io.ReadAll(sess.Reader())
	require.NoError(t, sess.Close())
	assert.Equal(t, "version=2", capturedProtocol)
}

func TestHTTPTransport_GitProtocolV0NoHeader(t *testing.T) {
	t.Parallel()
	var capturedProtocol string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedProtocol = r.Header.Get("Git-Protocol")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack", Protocol: protocol.V0}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	_, _ = io.ReadAll(sess.Reader())
	require.NoError(t, sess.Close())
	assert.Empty(t, capturedProtocol)
}

func TestHTTPTransport_UserAgent(t *testing.T) {
	t.Parallel()
	var capturedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack"}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	_, _ = io.ReadAll(sess.Reader())
	require.NoError(t, sess.Close())
	assert.Contains(t, capturedUA, "git/")
}

func TestHTTPTransport_URLBasicAuth(t *testing.T) {
	t.Parallel()
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	u, _ := url.Parse(srv.URL + "/repo.git")
	u.User = url.UserPassword("alice", "secret")
	req := &transport.Request{URL: u, Command: "git-upload-pack"}

	sess, err := tr.Open(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	_, _ = io.ReadAll(sess.Reader())
	require.NoError(t, sess.Close())
	assert.Contains(t, capturedAuth, "Basic ")
}

func TestHTTPTransport_ContextCancellation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tr := NewTransport(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	u, _ := url.Parse(srv.URL + "/repo.git")
	req := &transport.Request{URL: u, Command: "git-upload-pack"}

	sess, err := tr.Open(ctx, req)
	require.NoError(t, err)
	require.NoError(t, sess.Writer().Close())
	_, err = io.ReadAll(sess.Reader())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
