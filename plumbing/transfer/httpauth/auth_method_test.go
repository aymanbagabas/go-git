package httpauth

import (
	"net/http"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/transfer"
	"github.com/stretchr/testify/require"
)

func TestNewBasicAuth(t *testing.T) {
	a := &BasicAuth{"foo", "qux"}

	require.Equal(t, "http-basic-auth", a.Name())
	require.Equal(t, "http-basic-auth - foo:*******", a.String())
}

func TestNewTokenAuth(t *testing.T) {
	a := &TokenAuth{"OAUTH-TOKEN-TEXT"}

	require.Equal(t, "http-token-auth", a.Name())
	require.Equal(t, "http-token-auth - *******", a.String())

	tr, err := (&transfer.HTTPTransport{}).ConfigureAuthMethod(a)
	require.NoError(t, err)

	// Check header is set correctly
	req, err := http.NewRequest("GET", "https://github.com/git-fixtures/basic", nil)
	require.NoError(t, err)
	tr.(*transfer.HTTPTransport).AuthCallback(req)
	require.Equal(t, "Bearer OAUTH-TOKEN-TEXT", req.Header.Get("Authorization"))
}
