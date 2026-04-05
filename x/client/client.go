// Package client provides a convenience Client that resolves URL schemes
// to transport implementations and provides Handshake/Connect methods.
package client

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v6/x/transport"
	"github.com/go-git/go-git/v6/x/transport/file"
	xgit "github.com/go-git/go-git/v6/x/transport/git"
	xhttp "github.com/go-git/go-git/v6/x/transport/http"
	xssh "github.com/go-git/go-git/v6/x/transport/ssh"
)

// Options configures the Client with transport-specific settings.
type Options struct {
	File file.Options
	Git  xgit.Options
	SSH  xssh.Options
	HTTP xhttp.Options
}

// Client resolves URL schemes to transport implementations.
type Client struct {
	opts    Options
	schemes map[string]transport.Transport
}

// New creates a Client with built-in transports for file, git, ssh, http,
// and https schemes.
func New(opts Options) *Client {
	return &Client{
		opts:    opts,
		schemes: make(map[string]transport.Transport),
	}
}

// RegisterTransport registers a custom transport for the given URL scheme.
// This overrides any built-in transport for that scheme.
func (c *Client) RegisterTransport(scheme string, tr transport.Transport) {
	c.schemes[scheme] = tr
}

// Handshake resolves the transport for the request URL scheme and performs
// a pack protocol handshake.
func (c *Client) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	tr, err := c.resolve(req)
	if err != nil {
		return nil, err
	}
	return tr.Handshake(ctx, req)
}

// Connect resolves the transport for the request URL scheme and opens a
// raw full-duplex stream. Returns ErrConnectUnsupported if the transport
// does not implement Connectable (e.g. HTTP).
func (c *Client) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	tr, err := c.resolve(req)
	if err != nil {
		return nil, err
	}
	conn, ok := tr.(transport.Connectable)
	if !ok {
		return nil, transport.ErrConnectUnsupported
	}
	return conn.Connect(ctx, req)
}

// Transport returns the resolved Transport for the given URL scheme.
func (c *Client) Transport(scheme string) (transport.Transport, error) {
	if tr, ok := c.schemes[scheme]; ok {
		return tr, nil
	}
	return c.builtin(scheme)
}

// Close releases resources held by the client.
func (c *Client) Close() error {
	return nil
}

func (c *Client) resolve(req *transport.Request) (transport.Transport, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("transport: nil request or URL")
	}
	return c.Transport(req.URL.Scheme)
}

func (c *Client) builtin(scheme string) (transport.Transport, error) {
	switch scheme {
	case "file":
		return file.NewTransport(c.opts.File), nil
	case "git":
		return xgit.NewTransport(c.opts.Git), nil
	case "ssh":
		return xssh.NewTransport(c.opts.SSH), nil
	case "http", "https":
		return xhttp.NewTransport(c.opts.HTTP), nil
	default:
		return nil, fmt.Errorf("transport: unsupported scheme %q", scheme)
	}
}
