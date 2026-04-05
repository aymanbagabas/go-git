// Package http implements the HTTP transport for the new transport API.
package http

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	transport "github.com/go-git/go-git/v6/x/transport"
)

// NewFactory returns a transport.Factory that creates HTTP transports.
func NewFactory() transport.Factory {
	return func(opts transport.ClientOptions) transport.Transport {
		return &httpTransport{opts: opts}
	}
}

type httpTransport struct {
	opts transport.ClientOptions
}

func (t *httpTransport) Open(ctx context.Context, req *transport.Request) (transport.Session, error) {
	client := t.resolveClient()
	authorizer := t.opts.HTTP.Authorizer
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

		// Extract basic auth from URL userinfo if present.
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

// resolveClient returns the HTTP client to use, applying proxy settings
// from ProxyOptions if no custom client was provided.
func (t *httpTransport) resolveClient() *http.Client {
	if t.opts.HTTP.Client != nil {
		return t.opts.HTTP.Client
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()

	if t.opts.Proxy.HTTPProxy != nil {
		tr.Proxy = t.opts.Proxy.HTTPProxy
	}

	return &http.Client{Transport: tr}
}
