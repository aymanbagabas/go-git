package transfer

import (
	"context"
	"net"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// DefaultGitPort is the default port for Git TCP protocol.
const DefaultGitPort = "9418"

// DefaultGitTransport is the default Git TCP transport.
var DefaultGitTransport = &GitTransport{
	Dialer: proxy.FromEnvironmentUsing(DefaultDialer),
}

// GitTransport is a transport mechanism for the Git TCP protocol.
type GitTransport struct {
	Dialer proxy.Dialer
}

var (
	_ Transport          = &GitTransport{}
	_ DialerConfigurer   = &GitTransport{}
	_ ProxyURLConfigurer = &GitTransport{}
)

// Clone returns a new copy of the transport.
func (t *GitTransport) Clone() *GitTransport {
	return &GitTransport{
		Dialer: t.Dialer,
	}
}

// ConfigureDialer configures a new copy of the transport with the given
// dialer.
func (t *GitTransport) ConfigureDialer(d proxy.Dialer) (Transport, error) {
	t2 := t.Clone()
	t2.Dialer = d
	return t2, nil
}

// ConfigureProxyURL configures a new copy of the transport with the given
// proxy URL.
func (t *GitTransport) ConfigureProxyURL(url *url.URL) (Transport, error) {
	t2 := t.Clone()
	dialer, err := proxy.FromURL(url, t.Dialer)
	if err != nil {
		return nil, err
	}
	t2.Dialer = dialer
	return t2, nil
}

// Connect connects to a Git remote using the Git TCP protocol.
func (t *GitTransport) Connect(ctx context.Context, cmd *Cmd) (net.Conn, error) {
	if t.Dialer == nil {
		t.Dialer = &net.Dialer{}
	}

	host := cmd.URL.Host
	port := cmd.URL.Port()
	if port == "" {
		// No port specified, use default Git port.
		host = net.JoinHostPort(host, DefaultGitPort)
	}

	var conn net.Conn
	var err error
	if d, ok := t.Dialer.(proxy.ContextDialer); ok {
		conn, err = d.DialContext(ctx, "tcp", host)
	} else {
		conn, err = t.Dialer.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}

	return &GitConn{c: conn}, nil
}

// NewSession starts a new Git session for the given command.
func (t *GitTransport) NewSession(ctx context.Context, cmd *Cmd) (Session, error) {
	c, err := t.Connect(ctx, cmd)
	if err != nil {
		return nil, err
	}

	return NewPackSession(ctx, c, cmd)
}

// GitConn is a connection over TCP for Git transport.
type GitConn struct {
	c net.Conn
}

// Read reads data from the connection.
func (c *GitConn) Read(p []byte) (n int, err error) {
	return c.c.Read(p)
}

// Write writes data to the connection.
func (c *GitConn) Write(p []byte) (n int, err error) {
	return c.c.Write(p)
}

// Close closes the connection.
func (c *GitConn) Close() error {
	return c.c.Close()
}

// LocalAddr returns the local network address.
func (c *GitConn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *GitConn) RemoteAddr() net.Addr {
	return c.c.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *GitConn) SetDeadline(t time.Time) error {
	return c.c.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *GitConn) SetReadDeadline(t time.Time) error {
	return c.c.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *GitConn) SetWriteDeadline(t time.Time) error {
	return c.c.SetWriteDeadline(t)
}
