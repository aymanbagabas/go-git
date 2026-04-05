package transport

import (
	"context"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/crypto/ssh"
)

// DialContextFunc is the function signature for dialing network connections.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// ClientOptions holds the default configuration for a Client.
type ClientOptions struct {
	HTTP  HTTPOptions
	SSH   SSHOptions
	Dial  DialOptions
	Proxy ProxyOptions
}

// CallOptions holds per-call overrides. Nil fields inherit from client defaults.
type CallOptions struct {
	HTTP  *HTTPOptions
	SSH   *SSHOptions
	Dial  *DialOptions
	Proxy *ProxyOptions
}

// HTTPOptions configures HTTP-based transports.
type HTTPOptions struct {
	// Client is the underlying HTTP client. If nil, a default client is used.
	// TLS configuration (InsecureSkipVerify, custom CA bundles) should be
	// configured on the Client's Transport.
	Client *http.Client

	// Authorizer mutates outgoing HTTP requests to add authentication.
	Authorizer func(*http.Request) error
}

// SSHOptions configures SSH-based transports.
type SSHOptions struct {
	ClientConfig func(context.Context, *Request) (*ssh.ClientConfig, error)
}

// DialOptions configures stream dialing for SSH, Git TCP, and file transports.
type DialOptions struct {
	DialContext DialContextFunc
}

// ProxyOptions configures proxy behavior for both HTTP and stream transports.
type ProxyOptions struct {
	// HTTPProxy returns the proxy URL for a given HTTP request.
	// If nil, http.ProxyFromEnvironment is used for HTTP transports.
	// Use http.ProxyURL(u) for a fixed proxy URL.
	HTTPProxy func(*http.Request) (*url.URL, error)

	// DialProxy wraps a DialContext function to route stream connections
	// (SSH, git://) through a proxy. If nil, connections are made directly.
	DialProxy func(DialContextFunc) DialContextFunc
}

// resolveOptions merges call overrides onto client defaults. Call options
// replace (not deep merge) client defaults when non-nil.
func resolveOptions(defaults ClientOptions, overrides *CallOptions) ClientOptions {
	if overrides == nil {
		return defaults
	}
	if overrides.HTTP != nil {
		defaults.HTTP = *overrides.HTTP
	}
	if overrides.SSH != nil {
		defaults.SSH = *overrides.SSH
	}
	if overrides.Dial != nil {
		defaults.Dial = *overrides.Dial
	}
	if overrides.Proxy != nil {
		defaults.Proxy = *overrides.Proxy
	}
	return defaults
}
