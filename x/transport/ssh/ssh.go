// Package ssh implements the SSH transport for the new transport API.
package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	transport "github.com/go-git/go-git/v6/x/transport"
)

// DefaultPort is the default port for the SSH protocol.
const DefaultPort = 22

// DefaultSSHConfig is the reader used to access parameters stored in the
// system's ssh_config files. If nil all the ssh_config are ignored.
var DefaultSSHConfig Config = ssh_config.DefaultUserSettings

// Config is a reader of SSH configuration.
type Config interface {
	Get(alias, key string) string
}

// NewFactory returns a transport.Factory that creates SSH transports.
func NewFactory() transport.Factory {
	return func(opts transport.ClientOptions) transport.Transport {
		return &sshTransport{opts: opts}
	}
}

type sshTransport struct {
	opts transport.ClientOptions
}

func (t *sshTransport) Open(ctx context.Context, req *transport.Request) (transport.Session, error) {
	rwc, err := t.Connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewStreamSession(rwc), nil
}

func (t *sshTransport) Connect(ctx context.Context, req *transport.Request) (io.ReadWriteCloser, error) {
	config, err := t.resolveConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	hostWithPort := resolveHostWithPort(req)

	client, err := dial(ctx, "tcp", hostWithPort, t.opts, config)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	gitProtocol := transport.GitProtocolEnv(req.Protocol)
	if gitProtocol != "" {
		_ = session.Setenv("GIT_PROTOCOL", gitProtocol)
	}

	cmd := buildCommand(req)
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	if err := session.Start(cmd); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	return &sshConn{
		Reader:  stdoutPipe,
		Writer:  stdinPipe,
		session: session,
		client:  client,
	}, nil
}

func (t *sshTransport) resolveConfig(ctx context.Context, req *transport.Request) (*gossh.ClientConfig, error) {
	if t.opts.SSH.ClientConfig != nil {
		return t.opts.SSH.ClientConfig(ctx, req)
	}
	return nil, fmt.Errorf("ssh: no ClientConfig provider configured")
}

func dial(ctx context.Context, network, addr string, opts transport.ClientOptions, config *gossh.ClientConfig) (*gossh.Client, error) {
	var conn net.Conn
	var err error

	switch {
	case opts.Proxy.DialProxy != nil:
		dialFn := opts.Dial.DialContext
		if dialFn == nil {
			dialFn = (&net.Dialer{}).DialContext
		}
		wrappedDial := opts.Proxy.DialProxy(dialFn)
		conn, err = wrappedDial(ctx, network, addr)
	case opts.Dial.DialContext != nil:
		conn, err = opts.Dial.DialContext(ctx, network, addr)
	default:
		conn, err = proxy.Dial(ctx, network, addr)
	}
	if err != nil {
		return nil, err
	}

	c, chans, reqs, err := gossh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}
	return gossh.NewClient(c, chans, reqs), nil
}

func resolveHostWithPort(req *transport.Request) string {
	hostname := req.URL.Hostname()
	port := req.URL.Port()

	if DefaultSSHConfig != nil {
		if configHost := DefaultSSHConfig.Get(hostname, "Hostname"); configHost != "" {
			hostname = configHost
		}
		if port == "" {
			if configPort := DefaultSSHConfig.Get(req.URL.Hostname(), "Port"); configPort != "" {
				if _, err := strconv.Atoi(configPort); err == nil {
					port = configPort
				}
			}
		}
	}

	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}

	return net.JoinHostPort(hostname, port)
}

type sshConn struct {
	io.Reader
	io.Writer
	session *gossh.Session
	client  *gossh.Client
}

func (c *sshConn) Close() error {
	_ = c.session.Close()
	return c.client.Close()
}

// buildCommand constructs the remote command string from the request.
// For example: git-upload-pack '/repo.git'
// Or with args: git-lfs-authenticate '/repo.git' 'download'
func buildCommand(req *transport.Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s '%s'", req.Command, req.URL.Path)
	for _, arg := range req.Args {
		fmt.Fprintf(&b, " '%s'", arg)
	}
	return b.String()
}
