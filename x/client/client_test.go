package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/x/transport"
)

func TestNew_BuiltinSchemes(t *testing.T) {
	t.Parallel()

	c := New(Options{})
	defer c.Close()

	for _, scheme := range []string{"file", "git", "http", "https", "ssh"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err, "scheme %q should be registered", scheme)
		assert.NotNil(t, tr, "transport for %q should not be nil", scheme)
	}
}

func TestNew_ConnectableSchemes(t *testing.T) {
	t.Parallel()

	c := New(Options{})
	defer c.Close()

	for _, scheme := range []string{"file", "git", "ssh"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err)
		_, ok := tr.(transport.Connectable)
		assert.True(t, ok, "scheme %q should implement Connectable", scheme)
	}
}

func TestNew_HTTPNotConnectable(t *testing.T) {
	t.Parallel()

	c := New(Options{})
	defer c.Close()

	for _, scheme := range []string{"http", "https"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err)
		_, ok := tr.(transport.Connectable)
		assert.False(t, ok, "scheme %q should NOT implement Connectable", scheme)
	}
}

func TestNew_UnsupportedScheme(t *testing.T) {
	t.Parallel()

	c := New(Options{})
	defer c.Close()

	_, err := c.Transport("ftp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scheme")
}

func TestRegisterTransport(t *testing.T) {
	t.Parallel()

	c := New(Options{})
	defer c.Close()

	custom := &mockTransport{}
	c.RegisterTransport("custom", custom)

	tr, err := c.Transport("custom")
	require.NoError(t, err)
	assert.Equal(t, custom, tr)

	// Override builtin
	c.RegisterTransport("ssh", custom)
	tr, err = c.Transport("ssh")
	require.NoError(t, err)
	assert.Equal(t, custom, tr)
}

type mockTransport struct{}

func (m *mockTransport) Handshake(_ context.Context, _ *transport.Request) (transport.Session, error) {
	return nil, nil
}
