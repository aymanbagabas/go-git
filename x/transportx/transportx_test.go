package transportx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transport "github.com/go-git/go-git/v6/x/transport"
)

func TestNewClient_BuiltinSchemes(t *testing.T) {
	t.Parallel()

	c := NewClient()
	defer c.Close()

	for _, scheme := range []string{"file", "git", "http", "https", "ssh"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err, "scheme %q should be registered", scheme)
		assert.NotNil(t, tr, "transport for %q should not be nil", scheme)
	}
}

func TestNewClient_ConnectableSchemes(t *testing.T) {
	t.Parallel()

	c := NewClient()
	defer c.Close()

	for _, scheme := range []string{"file", "git", "ssh"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err)
		_, ok := tr.(transport.Connectable)
		assert.True(t, ok, "scheme %q should implement Connectable", scheme)
	}
}

func TestNewClient_HTTPNotConnectable(t *testing.T) {
	t.Parallel()

	c := NewClient()
	defer c.Close()

	for _, scheme := range []string{"http", "https"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err)
		_, ok := tr.(transport.Connectable)
		assert.False(t, ok, "scheme %q should NOT implement Connectable", scheme)
	}
}

func TestNewClient_CustomOptionsMerge(t *testing.T) {
	t.Parallel()

	c := NewClient(transport.WithScheme("custom", func(transport.ClientOptions) transport.Transport {
		return nil
	}))
	defer c.Close()

	tr, err := c.Transport("custom")
	require.NoError(t, err)
	assert.Nil(t, tr)

	tr, err = c.Transport("ssh")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}
