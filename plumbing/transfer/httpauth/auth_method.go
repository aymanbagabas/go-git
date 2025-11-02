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
func (a *BasicAuth) SetAuth(t transfer.Transport) error {
	if ht, ok := t.(*transfer.HTTPTransport); ok {
		if ht.Client == nil {
			ht.Client = &http.Client{}
		}
		if ht.Client.Transport == nil {
			ht.Client.Transport = http.DefaultTransport
		}
		ht.Client.Transport = &basicAuthRoundTripper{
			BasicAuth: a,
			Transport: ht.Client.Transport,
		}
		return nil
	}
	return ErrNotHTTPTransport
}

type basicAuthRoundTripper struct {
	*BasicAuth
	Transport http.RoundTripper
}

// RoundTrip implements the [http.RoundTripper] interface.
func (a *basicAuthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if a.Username != "" || a.Password != "" {
		r.SetBasicAuth(a.Username, a.Password)
	}
	return a.Transport.RoundTrip(r)
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
func (a *TokenAuth) SetAuth(t transfer.Transport) error {
	if ht, ok := t.(*transfer.HTTPTransport); ok {
		if ht.Client == nil {
			ht.Client = &http.Client{}
		}
		if ht.Client.Transport == nil {
			ht.Client.Transport = http.DefaultTransport
		}
		ht.Client.Transport = &tokenAuthRoundTripper{
			TokenAuth: a,
			Transport: ht.Client.Transport,
		}
		return nil
	}
	return ErrNotHTTPTransport
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

type tokenAuthRoundTripper struct {
	*TokenAuth
	Transport http.RoundTripper
}

// RoundTrip implements the [http.RoundTripper] interface.
func (a *tokenAuthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.Token))
	return a.Transport.RoundTrip(r)
}
