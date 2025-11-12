package transfer

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
	"golang.org/x/net/proxy"
)

// DefaultDialer is the default dialer used by transports. It has 30 seconds
// timeout and keep-alive.
var DefaultDialer = &net.Dialer{
	Timeout:   30 * time.Second,
	KeepAlive: 30 * time.Second,
}

// Runner represents a connection session to a Git remote. A session can
// implement an optional [StderrPiper] interface to provide access to the
// command's stderr output.

// Runner represents a client that can run Git commands. It provides access to
// the command's stdin and stdout streams, and allows starting and closing the
// commands.
//
// It can optionally implement the [StderrPiper] interface to provide access to
// stderr output.
type Runner interface {
	// Start starts the specified command. It does not wait for it to complete.
	// A context is provided to allow controlling cancellation and timeouts.
	Start(context.Context, *Cmd) error
	// StdinPipe returns a pipe that will be connected to the command's
	// standard input when the command starts. It should not be called after
	// [Session.Start]. The pipe should be closed when no more input is
	// expected.
	StdinPipe() (io.WriteCloser, error)
	// StdoutPipe returns a pipe that will be connected to the command's
	// standard output when the command starts. It should not be called after
	// [Session.Start].
	StdoutPipe() (io.Reader, error)
	// Close closes the session and releases any resources used by it. It will
	// block until the command exits.
	Close() error
}

// StderrPiper is an optional interface implemented by [Runner]s that support
// stderr pipes.
type StderrPiper interface {
	// StderrPipe returns a pipe that will be connected to the command's
	// standard error when the command starts. It should not be called after
	// [Session.Start].
	StderrPipe() (io.Reader, error)
}

// Connectable is a transport that can connect to a Git remote.
type Connectable interface {
	// Connect connects to a Git remote.
	Connect(ctx context.Context, remoteURL *url.URL) (Runner, error)
}

// Transport is a Git transport that can start sessions with a remote for a
// given command.
type Transport interface {
	// Handshake performs the initial handshake to establish a Git pack
	// transfer session for the given remote url and command.
	Handshake(ctx context.Context, remoteURL *url.URL, cmd *Cmd) (Session, error)
}

// TransportOld defines a Git transport mechanism.
type TransportOld interface {
	// Connect connects to a Git remote.
	Connect(ctx context.Context, cmd *Cmd) (net.Conn, error)

	// NewSession starts the negotiation with the remote to get a [Session] for
	// the given command.
	// A session is only suitable for a single "git-upload-pack" or
	// "git-receive-pack" command execution.
	NewSession(ctx context.Context, cmd *Cmd) (Session, error)
}

// ProxyURLConfigurer is implemented by transports that support proxy
// configuration.
type ProxyURLConfigurer interface {
	// ConfigureProxyURL configures the transport to use the given proxy URL.
	ConfigureProxyURL(url *url.URL) (TransportOld, error)
}

// DialerConfigurer is implemented by transports that support custom
// dialers.
type DialerConfigurer interface {
	// ConfigureDialer configures the transport to use the given dialer for
	// network connections.
	ConfigureDialer(dialer proxy.Dialer) (TransportOld, error)
}

// AuthMethodConfigurer is implemented by transports that support
// authentication configuration.
type AuthMethodConfigurer interface {
	// ConfigureAuthMethod configures the transport to use the given authentication
	// method.
	ConfigureAuthMethod(auth AuthMethod) (TransportOld, error)
}

// TLSConfigConfigurer is implemented by transports that support custom TLS
// configuration.
type TLSConfigConfigurer interface {
	// ConfigureTLSConfig configures the transport to use the given TLS
	// configuration for secure connections.
	ConfigureTLSConfig(tlsConfig *tls.Config) (TransportOld, error)
}

// Session is an open Git pack transfer session for a remote repository.
type Session interface {
	// StatelessRPC indicates that the connection is a half-duplex connection
	// and should operate in half-duplex mode i.e. performs a single read-write
	// cycle. This fits with the HTTP POST request process where session may
	// read the request, write a response, and exit.
	StatelessRPC() bool

	// Capabilities returns the advertised server capabilities.
	Capabilities() *capability.List

	// Version returns the discovered Git protocol version the server is
	// advertising.
	Version() protocol.Version

	// GetRemoteRefs returns the references advertised by the remote.
	// Using protocol v0 or v1, this returns the references advertised by the
	// remote during the handshake. Using protocol v2, this runs the ls-refs
	// command on the remote.
	// This will error if the session is not already established using
	// Handshake.
	GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)

	// Fetch sends a fetch-pack request to the server.
	Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error

	// Push sends a send-pack request to the server.
	Push(ctx context.Context, st storage.Storer, req *PushRequest) error

	// Close closes the connection.
	Close() error
}

// hostPort returns the host:port for the given URL. If the URL does not
// specify a port, the given defaultPort is used.
// If defaultPort is an empty string and the URL does not specify a port, the
// url host is returned.
func hostPort(u *url.URL, defaultPort string) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if defaultPort == "" {
			return host
		}
		port = defaultPort
	}
	return net.JoinHostPort(host, port)
}
