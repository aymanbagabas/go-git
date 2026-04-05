// Package http implements the HTTP transport for the new transport API.
package http

import (
	"net/http"
	"net/url"
)

// Options configures the HTTP transport.
type Options struct {
	// Client is the underlying HTTP client. If nil, a default client is used.
	// TLS configuration (InsecureSkipVerify, custom CA bundles) should be
	// configured on the Client's Transport.
	Client *http.Client

	// Authorizer mutates outgoing HTTP requests to add authentication.
	Authorizer func(*http.Request) error

	// HTTPProxy returns the proxy URL for a given HTTP request.
	// If nil, http.ProxyFromEnvironment is used when no custom Client
	// is provided.
	HTTPProxy func(*http.Request) (*url.URL, error)
}

// Transport implements the http:// and https:// transport protocol.
type Transport struct {
	opts Options
}

// NewTransport creates an HTTP transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

func (t *Transport) resolveClient() *http.Client {
	if t.opts.Client != nil {
		return t.opts.Client
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()

	if t.opts.HTTPProxy != nil {
		tr.Proxy = t.opts.HTTPProxy
	}

	return &http.Client{Transport: tr}
}
