package transfer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
)

// Client is a Git client that can fetch and push to a Git server.
type Client struct {
	t Transport
}

// NewClient creates a new Git client for the given remote url and auth. If
// auth is nil, no authentication will be used.
func NewClient(url *url.URL, auth AuthMethod) (*Client, error) {
	if url == nil {
		return nil, fmt.Errorf("url cannot be nil")
	}
	t, err := GetTransport(url.Scheme)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		if ac, ok := t.(AuthMethodConfigurer); ok {
			t, err = ac.ConfigureAuthMethod(auth)
			if err != nil {
				return nil, err
			}
		}
	}
	return &Client{t: t}, nil
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
