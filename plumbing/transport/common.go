// Package transport implements the git pack protocol with a pluggable
// This is a low-level package to implement new transports. Use a concrete
// implementation instead (e.g. http, file, ssh).
//
// A simple example of usage can be found in the file package.
package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
)

const (
	readErrorSecondsTimeout = 10
)

var (
	// ErrUnsupportedVersion is returned when the protocol version is not
	// supported.
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
	// ErrUnsupportedService is returned when the service is not supported.
	ErrUnsupportedService = errors.New("unsupported service")
	// ErrInvalidResponse is returned when the response is invalid.
	ErrInvalidResponse = errors.New("invalid response")
	// ErrTimeoutExceeded is returned when the timeout is exceeded.
	ErrTimeoutExceeded = errors.New("timeout exceeded")
	// ErrPackedObjectsNotSupported is returned when the server does not support
	// packed objects.
	ErrPackedObjectsNotSupported = errors.New("packed objects not supported")
	// stdErrSkipPattern is used for skipping lines from a command's stderr output.
	// Any line matching this pattern will be skipped from further
	// processing and not be returned to calling code.
	stdErrSkipPattern = regexp.MustCompile("^remote:( =*){0,1}$")
)

// RemoteError represents an error returned by the remote.
// TODO: embed error
type RemoteError struct {
	Reason string
}

// Error implements the error interface.
func (e *RemoteError) Error() string {
	return e.Reason
}

// NewRemoteError creates a new RemoteError.
func NewRemoteError(reason string) error {
	return &RemoteError{Reason: reason}
}

// Conn represents a session endpoint connection.
type Conn interface {
	io.ReadWriteCloser
}

var _ io.Closer = Conn(nil)

// FetchRequest contains the parameters for a fetch-pack request.
// This is used during the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
type FetchRequest struct {
	// Progress is the progress sideband.
	Progress sideband.Progress

	// Wants is the list of references to fetch.
	// TODO: Build this slice in the transport package.
	Wants []plumbing.Hash

	// Haves is the list of references the client already has.
	// TODO: Build this slice in the transport package.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// Filter holds the filters to be applied when deciding what
	// objects will be added to the packfile.
	Filter packp.Filter

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool
}

// PushRequest contains the parameters for a push request.
type PushRequest struct {
	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Commands is the list of push commands to be sent to the server.
	// TODO: build the Commands slice in the transport package.
	Commands []*packp.Command

	// Progress is the progress sideband.
	Progress sideband.Progress

	// Options is a set of push-options to be sent to the server during push.
	Options []string

	// Atomic indicates an atomic push.
	// If the server supports atomic push, it will update the refs in one
	// atomic transaction. Either all refs are updated or none.
	Atomic bool
}

// Session is a Git protocol transfer session.
// This is used by all protocols.
type Session interface {
	// Handshake starts the negotiation with the remote to get version if not
	// already connected.
	// Params are the optional extra parameters to be sent to the server. Use
	// this to send the protocol version of the client and any other extra parameters.
	Handshake(ctx context.Context, service Service, params ...string) (Conn, error)

	// StatelessRPC indicates that the connection is a half-duplex connection
	// and should operate in half-duplex mode i.e. performs a single read-write
	// cycle. This fits with the HTTP POST request process where session may
	// read the request, write a response, and exit.
	StatelessRPC() bool

	// Close closes the session and any underlying resources.
	Close() error
}

// Capabilities returns the list of capabilities supported by the server.
func Capabilities(sess Session) (*capability.List, error) {
	if s, ok := sess.(interface {
		Capabilities() *capability.List
	}); ok {
		return s.Capabilities(), nil
	}
	return nil, fmt.Errorf("transport: session does not implement Capabilities")
}

// Version returns the protocol version supported by the server.
func Version(sess Session) (protocol.Version, error) {
	if s, ok := sess.(interface {
		Version() protocol.Version
	}); ok {
		return s.Version(), nil
	}
	return protocol.V0, fmt.Errorf("transport: session does not implement Version")
}

// GetRemoteRefs returns the references advertised by the remote.
// Using protocol v0 or v1, this returns the references advertised by the
// remote during the handshake. Using protocol v2, this runs the ls-refs
// command on the remote.
// This will error if the session is not already established using
// Handshake.
func GetRemoteRefs(ctx context.Context, sess Session) ([]*plumbing.Reference, error) {
	if s, ok := sess.(interface {
		GetRemoteRefs(context.Context) ([]*plumbing.Reference, error)
	}); ok {
		return s.GetRemoteRefs(ctx)
	}
	return nil, fmt.Errorf("transport: session does not implement GetRemoteRefs")
}

// Fetch sends a fetch-pack request to the server.
func Fetch(ctx context.Context, sess Session, req *FetchRequest) error {
	if s, ok := sess.(interface {
		Fetch(context.Context, *FetchRequest) error
	}); ok {
		return s.Fetch(ctx, req)
	}
	return fmt.Errorf("transport: session does not implement Fetch")
}

// Push sends a send-pack request to the server.
func Push(ctx context.Context, sess Session, req *PushRequest) error {
	if s, ok := sess.(interface {
		Push(context.Context, *PushRequest) error
	}); ok {
		return s.Push(ctx, req)
	}
	return fmt.Errorf("transport: session does not implement Push")
}

// Runner represents a transport that can run commands for a given endpoint and
// auth.
type Runner interface {
	// Run runs the command for the given endpoint and auth method. It mutates
	// the passed cmd to set its methods and Sys field as needed by the
	// transport implementation.
	Run(ctx context.Context, cmd *Cmd, ep *Endpoint, auth AuthMethod) error
}

// Cmd represents a command that can be started. The [Cmd] type is modeled
// after [exec.Cmd] but is simplified to only the methods needed by go-git.
type Cmd struct {
	StderrPipe func() (io.Reader, error)
	StdinPipe  func() (io.WriteCloser, error)
	StdoutPipe func() (io.Reader, error)
	Start      func() error
	Close      func() error
	Sys        interface{} // underlying type depends on the transport implementation

	Path string
	Args []string
	Env  []string
}

// Command creates a new [Cmd] instance.
func Command(name string, args ...string) *Cmd {
	return &Cmd{
		Path: name,
		Args: append([]string{name}, args...),
	}
}

type client struct {
	runr Runner
}

// NewPackTransport creates a new client using the given Commander.
func NewPackTransport(runner Runner) Transport {
	return &client{runner}
}

// NewSession returns a new session for an endpoint.
func (c *client) NewSession(st storage.Storer, ep *Endpoint, auth AuthMethod) (Session, error) {
	return NewPackSession(st, ep, auth, c.runr)
}

// SupportedProtocols returns a list of supported Git protocol versions by
// the transport client.
func (c *client) SupportedProtocols() []protocol.Version {
	return []protocol.Version{
		protocol.V0,
		protocol.V1,
	}
}
