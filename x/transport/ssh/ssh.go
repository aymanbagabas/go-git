// Package ssh implements the SSH transport for the new transport API.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/kevinburke/ssh_config"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	"github.com/go-git/go-git/v6/utils/ioutil"
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

// Options configures the SSH transport.
type Options struct {
	// ClientConfig provides SSH client configuration for each request.
	ClientConfig func(context.Context, *transport.Request) (*gossh.ClientConfig, error)

	// DialContext is the function used to establish TCP connections.
	// If nil, golang.org/x/net/proxy.Dial is used.
	DialContext transport.DialContextFunc

	// DialProxy wraps DialContext to route connections through a proxy.
	// If nil, connections are made directly.
	DialProxy func(transport.DialContextFunc) transport.DialContextFunc
}

// Transport implements the ssh:// transport protocol.
type Transport struct {
	opts Options
}

// NewTransport creates an SSH transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	conn, err := t.connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewConn(conn.stdout, conn.stdin, conn.Close), nil
}

func (t *Transport) connect(ctx context.Context, req *transport.Request) (*sshConn, error) {
	config, err := t.resolveConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	hostWithPort := resolveHostWithPort(req)

	client, err := t.dial(ctx, "tcp", hostWithPort, config)
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

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	conn := &sshConn{
		stdout:  stdoutPipe,
		stdin:   stdinPipe,
		session: session,
		client:  client,
	}

	// Read stderr in background — surface as RemoteError on Close or
	// when the caller encounters an error reading stdout.
	go func() {
		var buf bytes.Buffer
		_, _ = ioutil.CopyBufferPool(&buf, stderrPipe)
		conn.stderrBuf.Store(&buf)
	}()

	cmd := buildCommand(req)
	if err := session.Start(cmd); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	return conn, nil
}

func (t *Transport) resolveConfig(ctx context.Context, req *transport.Request) (*gossh.ClientConfig, error) {
	if t.opts.ClientConfig != nil {
		return t.opts.ClientConfig(ctx, req)
	}
	return nil, fmt.Errorf("ssh: no ClientConfig provider configured")
}

func (t *Transport) dial(ctx context.Context, network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
	var conn net.Conn
	var err error

	switch {
	case t.opts.DialProxy != nil:
		dialFn := t.opts.DialContext
		if dialFn == nil {
			dialFn = (&net.Dialer{}).DialContext
		}
		conn, err = t.opts.DialProxy(dialFn)(ctx, network, addr)
	case t.opts.DialContext != nil:
		conn, err = t.opts.DialContext(ctx, network, addr)
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
	stdout    io.Reader
	stdin     io.WriteCloser
	session   *gossh.Session
	client    *gossh.Client
	stderrBuf atomic.Pointer[bytes.Buffer]
}

func (c *sshConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if err != nil {
		// If stdout read fails, check stderr for a more useful error.
		if stderrErr := c.stderr(); stderrErr != nil {
			return n, stderrErr
		}
	}
	return n, err
}

func (c *sshConn) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *sshConn) Close() error {
	_ = c.stdin.Close()
	_ = c.session.Close()
	err := c.client.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	// If there was stderr output, surface it as the error.
	if stderrErr := c.stderr(); stderrErr != nil {
		return stderrErr
	}
	return err
}

// stderr returns stderr content as a RemoteError if non-empty.
func (c *sshConn) stderr() error {
	buf := c.stderrBuf.Load()
	if buf == nil {
		return nil
	}
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return nil
	}
	return transport.NewRemoteError(s)
}

func buildCommand(req *transport.Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s '%s'", req.Command, req.URL.Path)
	for _, arg := range req.Args {
		fmt.Fprintf(&b, " '%s'", arg)
	}
	return b.String()
}
