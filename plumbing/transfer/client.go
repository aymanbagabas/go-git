package transfer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Client is a Git client that can fetch and push to a Git server.
type Client struct {
	// HTTP transport configurations.
	HTTPClient       func(remoteURL *url.URL) (*http.Client, error)
	HTTPAuthCallback func(remoteURL *url.URL, req *http.Request) error

	// SSH transport configurations.
	SSHClientConfig func(remoteURL *url.URL) (*ssh.ClientConfig, error)

	// Git and SSH transport dialer configurations.
	Dialer func(remoteURL *url.URL) (net.Dialer, error)

	// File transport configurations.
	FileLoader func(remoteURL *url.URL) (Loader, error)

	mu         sync.RWMutex
	transports map[string]func(remoteURL *url.URL) Transport
}

// RegisterProtocol registers a custom transport for the given protocol.
func (c *Client) RegisterProtocol(protocol string, transportFunc func(remoteURL *url.URL) Transport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transports == nil {
		c.transports = make(map[string]func(remoteURL *url.URL) Transport)
	}
	c.transports[protocol] = transportFunc
}

// Connect connects to a Git remote repository using the appropriate transport.
func (c *Client) Connect(ctx context.Context, remoteURL *url.URL) (Runner, error) {
	transport, ok := c.getTransport(remoteURL.Scheme, remoteURL)
	if ok {
		if runner, ok := transport.(Connectable); ok {
			return runner.Connect(ctx, remoteURL)
		}
		return nil, fmt.Errorf("transport for protocol %q does not support Connect", remoteURL.Scheme)
	}

	// Use default transports
	panic("not implemented yet")
}

// Handshake performs the initial handshake to establish a Git pack
// transfer session for the given remote url and command.
func (c *Client) Handshake(ctx context.Context, remoteURL *url.URL, cmd *Cmd) (Session, error) {
	transport, ok := c.getTransport(remoteURL.Scheme, remoteURL)
	if ok {
		return transport.Handshake(ctx, remoteURL, cmd)
	}

	// Use default transports
	panic("not implemented yet")
}

// getTransport gets the transport for the given protocol.
func (c *Client) getTransport(protocol string, remoteURL *url.URL) (Transport, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if transportFunc, ok := c.transports[protocol]; ok {
		return transportFunc(remoteURL), true
	}
	return nil, false
}

// SetProxyURL sets the proxy URL for the Git client and transports.
func (c *Client) SetProxyURL(proxyURL *url.URL) error {
	if proxyURL == nil {
		return fmt.Errorf("proxy URL cannot be nil")
	}
	if pc, ok := c.t.(ProxyURLConfigurer); ok {
		t, err := pc.ConfigureProxyURL(proxyURL)
		if err != nil {
			return err
		}
		c.t = t
	}
	return nil
}

// SetTLSConfig sets the TLS configuration for the Git client and transports.
func (c *Client) SetTLSConfig(tlsConfig *tls.Config) error {
	if tlsConfig == nil {
		return fmt.Errorf("TLS config cannot be nil")
	}
	if tc, ok := c.t.(TLSConfigConfigurer); ok {
		t, err := tc.ConfigureTLSConfig(tlsConfig)
		if err != nil {
			return err
		}
		c.t = t
	}
	return nil
}

// Connect opens a connection to a Git remote repository.
func (c *Client) Connect(ctx context.Context, cmd *Cmd) (net.Conn, error) {
	return c.t.Connect(ctx, cmd)
}

// NewSession starts a new session for either a "git-upload-pack" or
// "git-receive-pack" service.
func (c *Client) NewSession(ctx context.Context, cmd *Cmd) (Session, error) {
	return c.t.NewSession(ctx, cmd)
}

// NewProxyURL creates a new [url.URL] from the given proxy information. The
// username and password are optional and can be empty strings.
func NewProxyURL(rawURL, username, password string) (*url.URL, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("proxy URL cannot be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if username != "" {
		if password != "" {
			u.User = url.UserPassword(username, password)
		} else {
			u.User = url.User(username)
		}
	}
	return u, nil
}
