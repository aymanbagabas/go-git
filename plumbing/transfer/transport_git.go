package transfer

import (
	"context"
	"net"
)

// DefaultGitPort is the default port for Git TCP protocol.
const DefaultGitPort = "9418"

// GitTransport is a transport mechanism for the Git TCP protocol.
type GitTransport struct {
	Dialer *net.Dialer
}

// Connect connects to a Git remote using the Git TCP protocol.
func (t *GitTransport) Connect(ctx context.Context, cmd *Cmd) (Conn, error) {
	if t.Dialer == nil {
		t.Dialer = &net.Dialer{}
	}

	host := cmd.URL.Host
	port := cmd.URL.Port()
	if port == "" {
		// No port specified, use default Git port.
		host = net.JoinHostPort(host, DefaultGitPort)
	}

	conn, err := t.Dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}

	return &GitConn{Conn: conn}, nil
}

// GitConn is a connection over TCP for Git transport.
type GitConn struct {
	net.Conn
}

// IsStateless indicates whether the connection is stateless.
func (c *GitConn) IsStateless() bool {
	return false
}
