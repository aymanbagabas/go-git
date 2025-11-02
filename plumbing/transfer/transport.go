package transfer

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"sync"
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

// Transport defines a Git transport mechanism.
type Transport interface {
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
	ConfigureProxyURL(url *url.URL) (Transport, error)
}

// DialerConfigurer is implemented by transports that support custom
// dialers.
type DialerConfigurer interface {
	// ConfigureDialer configures the transport to use the given dialer for
	// network connections.
	ConfigureDialer(dialer proxy.Dialer) (Transport, error)
}

// AuthMethodConfigurer is implemented by transports that support
// authentication configuration.
type AuthMethodConfigurer interface {
	// ConfigureAuthMethod configures the transport to use the given authentication
	// method.
	ConfigureAuthMethod(auth AuthMethod) (Transport, error)
}

// TLSConfigConfigurer is implemented by transports that support custom TLS
// configuration.
type TLSConfigConfigurer interface {
	// ConfigureTLSConfig configures the transport to use the given TLS
	// configuration for secure connections.
	ConfigureTLSConfig(tlsConfig *tls.Config) (Transport, error)
}

// Session represents a session endpoint connection.
type Session interface {
	// Close closes the connection.
	Close() error

	// Capabilities returns the list of capabilities supported by the server.
	Capabilities() *capability.List

	// Version returns the Git protocol version the server supports.
	Version() protocol.Version

	// StatelessRPC indicates that the connection is a half-duplex connection
	// and should operate in half-duplex mode i.e. performs a single read-write
	// cycle. This fits with the HTTP POST request process where session may
	// read the request, write a response, and exit.
	StatelessRPC() bool

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
}

var (
	// registry are the protocols supported by the package.
	registry = map[string]Transport{}
	mtx      sync.RWMutex
)

// RegisterProtocol registers a protocol to be supported by the transport
// layer.
func RegisterProtocol(scheme string, c Transport) {
	mtx.Lock()
	registerProtocol(scheme, c)
	mtx.Unlock()
}

func registerProtocol(scheme string, c Transport) {
	registry[scheme] = c
}

// GetTransport returns the appropriate [Transport] for the given protocol.
func GetTransport(scheme string) (Transport, error) {
	mtx.RLock()
	defer mtx.RUnlock()
	f, ok := registry[scheme]
	if !ok || f == nil {
		return nil, ErrUnsupportedTransport
	}
	return f, nil
}

func init() {
	// Default transports supported by go-git.
	mtx.Lock()
	registerProtocol("file", DefaultFileTransport)
	registerProtocol("git", DefaultGitTransport)
	registerProtocol("http", DefaultHTTPTransport)
	registerProtocol("https", DefaultHTTPTransport)
	registerProtocol("ssh", DefaultSSHTransport)
	mtx.Unlock()
}

func tryCloneProxy(p proxy.Dialer, d *net.Dialer) proxy.Dialer {
	if _, ok := p.(*proxy.PerHost); ok {
		// We know this was created using [proxy.FromEnvironmentUsing], so
		// we can create a new one with the cloned Dialer.
		return proxy.FromEnvironmentUsing(d)
	}
	// XXX: We cannot deep-copy arbitrary proxy.Dialer implementations.
	// Just assign the same instance and hope for the best.
	return p
}
