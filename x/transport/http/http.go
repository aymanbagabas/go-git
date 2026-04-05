// Package http implements the HTTP transport for the new transport API.
package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	transport "github.com/go-git/go-git/v6/x/transport"
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

func (t *Transport) Open(ctx context.Context, req *transport.Request) (transport.Session, error) {
	client := t.resolveClient()
	authorizer := t.opts.Authorizer
	gitProtocol := transport.GitProtocolEnv(req.Protocol)

	return transport.NewHTTPSession(func(body io.Reader) (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL.String(), body)
		if err != nil {
			return nil, fmt.Errorf("http transport: %w", err)
		}

		httpReq.Header.Set("User-Agent", capability.DefaultAgent())

		if gitProtocol != "" {
			httpReq.Header.Set("Git-Protocol", gitProtocol)
		}

		if req.URL.User != nil {
			password, _ := req.URL.User.Password()
			httpReq.SetBasicAuth(req.URL.User.Username(), password)
		}

		if authorizer != nil {
			if err := authorizer(httpReq); err != nil {
				return nil, fmt.Errorf("http transport: authorize: %w", err)
			}
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("http transport: %w", err)
		}

		return resp, nil
	}), nil
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
