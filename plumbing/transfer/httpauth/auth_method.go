package httpauth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-git/go-git/v6/plumbing/transfer"
)

// ErrNotHTTPTransport is returned when the auth method is used with a non-HTTP
// transport.
var ErrNotHTTPTransport = errors.New("the auth method can only be used with HTTP transport")

// BasicAuth represent a HTTP basic auth
type BasicAuth struct {
	Username, Password string
}

// SetAuth sets the basic auth header on the transport.
func (a *BasicAuth) SetAuth(t transfer.TransportOld) error {
	ht, ok := t.(*transfer.HTTPTransport)
	if a == nil || !ok {
		return ErrNotHTTPTransport
	}
	ht.AuthCallback = func(r *http.Request) {
		r.SetBasicAuth(a.Username, a.Password)
	}
	return nil
}

// Name is name of the auth
func (a *BasicAuth) Name() string {
	return "http-basic-auth"
}

func (a *BasicAuth) String() string {
	masked := "*******"
	if a.Password == "" {
		masked = "<empty>"
	}

	return fmt.Sprintf("%s - %s:%s", a.Name(), a.Username, masked)
}

// TokenAuth implements an http.AuthMethod that can be used with http transport
// to authenticate with HTTP token authentication (also known as bearer
// authentication).
//
// IMPORTANT: If you are looking to use OAuth tokens with popular servers (e.g.
// GitHub, Bitbucket, GitLab) you should use BasicAuth instead. These servers
// use basic HTTP authentication, with the OAuth token as user or password.
// Check the documentation of your git server for details.
type TokenAuth struct {
	Token string
}

// SetAuth sets the token auth header on the transport.
func (a *TokenAuth) SetAuth(t transfer.TransportOld) error {
	ht, ok := t.(*transfer.HTTPTransport)
	if a == nil || !ok {
		return ErrNotHTTPTransport
	}
	ht.AuthCallback = func(r *http.Request) {
		r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.Token))
	}
	return nil
}

// Name is name of the auth
func (a *TokenAuth) Name() string {
	return "http-token-auth"
}

func (a *TokenAuth) String() string {
	masked := "*******"
	if a.Token == "" {
		masked = "<empty>"
	}
	return fmt.Sprintf("%s - %s", a.Name(), masked)
}
