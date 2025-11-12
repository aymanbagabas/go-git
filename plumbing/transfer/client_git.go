package transfer

import (
	"context"
	"io"
	"net"
	"net/url"

	"github.com/go-git/go-git/v6/utils/ioutil"
)

// DefaultGitPort is the default port for Git TCP protocol.
const DefaultGitPort = "9418"

// DefaultGitTransport is the default Git TCP transport.
var DefaultGitTransport = &GitClient{
	// Dialer: proxy.FromEnvironmentUsing(DefaultDialer),
}

// GitClient is a client that can connect to a Git TCP remote.
type GitClient struct {
	Conn net.Conn
}

var _ Connectable = &GitClient{}

// Connect connects to a Git remote using the Git TCP protocol.
func (t *GitClient) Connect(ctx context.Context, remoteURL *url.URL) (Runner, error) {
	if t.Conn == nil {
		panic("Git client requires a net.Conn")
	}
	return &GitRunner{
		conn: t.Conn,
		url:  remoteURL,
	}, nil
}

// GitRunner is a Git command runner over a Git TCP connection.
type GitRunner struct {
	conn net.Conn
	url  *url.URL
}

var _ Runner = &GitRunner{}

// Close implements Session.
func (s *GitRunner) Close() error {
	return s.conn.Close()
}

// Start implements Session.
func (s *GitRunner) Start(ctx context.Context, cmd *Cmd) error {
	host := hostPort(s.url, DefaultGitPort)
	req := buildGitCommand(cmd, host)
	w := ioutil.NewContextWriteCloser(ctx, s.conn)
	if err := req.Encode(w); err != nil {
		_ = s.Close()
		return err
	}

	return nil
}

// StdinPipe implements Session.
func (s *GitRunner) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(s.conn), nil
}

// StdoutPipe implements Session.
func (s *GitRunner) StdoutPipe() (io.Reader, error) {
	return s.conn, nil
}
