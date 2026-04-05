package transport

import (
	"context"
	"fmt"
)

// Factory creates a Transport from resolved client options.
type Factory func(ClientOptions) Transport

// ClientOption configures a Client during construction.
type ClientOption func(*clientConfig)

type clientConfig struct {
	opts     ClientOptions
	schemes  map[string]Factory
	builtins bool
}

// WithHTTP sets the HTTP options for the client.
func WithHTTP(opts HTTPOptions) ClientOption {
	return func(c *clientConfig) {
		c.opts.HTTP = opts
	}
}

// WithSSH sets the SSH options for the client.
func WithSSH(opts SSHOptions) ClientOption {
	return func(c *clientConfig) {
		c.opts.SSH = opts
	}
}

// WithDial sets the dial options for the client.
func WithDial(opts DialOptions) ClientOption {
	return func(c *clientConfig) {
		c.opts.Dial = opts
	}
}

// WithProxy sets the proxy options for the client.
func WithProxy(opts ProxyOptions) ClientOption {
	return func(c *clientConfig) {
		c.opts.Proxy = opts
	}
}

// WithScheme registers a transport factory for the given URL scheme.
func WithScheme(scheme string, factory Factory) ClientOption {
	return func(c *clientConfig) {
		c.schemes[scheme] = factory
	}
}

// WithoutBuiltins disables the automatic registration of built-in
// transport factories (http, https, ssh, git, file).
func WithoutBuiltins() ClientOption {
	return func(c *clientConfig) {
		c.builtins = false
	}
}

// Client is an immutable transport client. It holds default configuration
// and a scheme-to-factory registry. Client is safe for concurrent use.
//
// Use NewClient to construct a Client. Configuration and scheme registration
// happen at construction time only.
type Client struct {
	defaults ClientOptions
	schemes  map[string]Factory
}

// NewClient creates an immutable Client. By default, built-in transport
// factories for http, https, ssh, git, and file are registered.
// Use ClientOption values to customize defaults and override or extend
// the scheme registry.
func NewClient(opts ...ClientOption) *Client {
	cfg := clientConfig{
		schemes:  make(map[string]Factory),
		builtins: true,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.builtins {
		registerBuiltins(cfg.schemes)
	}

	return &Client{
		defaults: cfg.opts,
		schemes:  cfg.schemes,
	}
}

// Open resolves the transport for the request URL scheme and opens a Session.
func (c *Client) Open(ctx context.Context, req *Request, opts ...CallOption) (Session, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("transport: nil request or URL")
	}

	factory, ok := c.schemes[req.URL.Scheme]
	if !ok {
		return nil, fmt.Errorf("transport: unsupported scheme %q", req.URL.Scheme)
	}

	resolved := c.resolveCallOptions(opts)
	t := factory(resolved)
	return t.Open(ctx, req)
}

// Transport resolves and returns the Transport for the given URL scheme
// with the given call options applied. This is useful for adapters that
// need direct access to the transport (e.g., to check for Connectable).
func (c *Client) Transport(scheme string, opts ...CallOption) (Transport, error) {
	factory, ok := c.schemes[scheme]
	if !ok {
		return nil, fmt.Errorf("transport: unsupported scheme %q", scheme)
	}

	resolved := c.resolveCallOptions(opts)
	return factory(resolved), nil
}

// Close releases resources held by the client.
func (c *Client) Close() error {
	return nil
}

// CallOption is a per-call override applied when opening a session.
type CallOption func(*CallOptions)

// WithCallHTTP overrides HTTP options for a single call.
func WithCallHTTP(opts HTTPOptions) CallOption {
	return func(c *CallOptions) {
		c.HTTP = &opts
	}
}

// WithCallSSH overrides SSH options for a single call.
func WithCallSSH(opts SSHOptions) CallOption {
	return func(c *CallOptions) {
		c.SSH = &opts
	}
}

// WithCallDial overrides dial options for a single call.
func WithCallDial(opts DialOptions) CallOption {
	return func(c *CallOptions) {
		c.Dial = &opts
	}
}

// WithCallProxy overrides proxy options for a single call.
func WithCallProxy(opts ProxyOptions) CallOption {
	return func(c *CallOptions) {
		c.Proxy = &opts
	}
}

func (c *Client) resolveCallOptions(opts []CallOption) ClientOptions {
	if len(opts) == 0 {
		return c.defaults
	}
	var co CallOptions
	for _, opt := range opts {
		opt(&co)
	}
	return resolveOptions(c.defaults, &co)
}

// registerBuiltins registers default transport factories.
func registerBuiltins(schemes map[string]Factory) {
	// File transport is registered with a nil loader (uses DefaultLoader).
	// The import is deferred to a separate file to avoid circular
	// dependencies during the transition.
	//
	// Actual wiring lives in builtins.go.
}
