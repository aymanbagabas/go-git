package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
)

// NewSession creates a new session for the given endpoint and authentication method.
func NewSession(st storage.Storer, ep *Endpoint, auth AuthMethod) (*Session, error) {
	t, err := Get(ep.Protocol)
	if err != nil {
		return nil, err
	}

	return t.NewSession(st, ep, auth)
}

// Session represents a session for a Git transport, which includes the
// connection to the remote server, performing fetch and push operations via
// [Session] options.
type Session struct {
	// Progress is the progress sideband.
	Progress sideband.Progress

	// Depth is the depth of the fetch.
	Depth int

	// Filter holds the filters to be applied when deciding what
	// objects will be added to the packfile.
	Filter packp.Filter

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool

	// PushOptions is a set of push-options to be sent to the server during
	// push.
	PushOptions []string

	// Atomic indicates an atomic push.
	// If the server supports atomic push, it will update the refs in one
	// atomic transaction. Either all refs are updated or none.
	Atomic bool

	conn      Conn
	ver       protocol.Version
	caps      *capability.List
	stateless bool
}

var _ Conn = &Session{}

// StatelessRPC returns whether the transport is stateless and operating in
// half-duplex.
func (s *Session) StatelessRPC() bool {
	if s.conn == nil {
		return false
	}
	return s.conn.StatelessRPC()
}

// Read reads data from the connection.
func (s *Session) Read(p []byte) (n int, err error) {
	if s.conn == nil {
		return 0, io.EOF
	}
	return s.conn.Read(p)
}

// Write writes data to the connection.
func (s *Session) Write(p []byte) (n int, err error) {
	if s.conn == nil {
		return 0, io.EOF
	}
	return s.conn.Write(p)
}

// Close closes the connection.
func (s *Session) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// Cmd represents the command used to connect to the remote server.
type Cmd struct {
	Service Service
	Env     []string
	Args    []string
}

// Connect establishes a connection to the remote server. Env can be used to
// pass environment variables like GIT_PROTOCOL to specify the protocol to use.
// Passing extra arguments would append the arguments to the command line when
// connecting.
func (s *Session) Connect(ctx context.Context, cmd Cmd) (Conn, error)

// Version returns the server Git protocol version.
func (s *Session) Version() (protocol.Version, error)

// Capabilities returns the server capabilities.
func (s *Session) Capabilities() (*capability.List, error)

// GetRemoteRefs retrieves the remote references discovered during the
// connection process.
func (s *Session) GetRemoteRefs() ([]*plumbing.Reference, error)

// Fetch sends a fetch-pack request to the server.
func (s *Session) Fetch(ctx context.Context, st storage.Storer, refs any) error

// Push sends a send-pack request to the server.
func (s *Session) Push(ctx context.Context, st storage.Storer, refs any) error

// Archive retrieves an archive from the remote server.
// func (s *Session) Archive(ctx context.Context, req *ArchiveRequest) (io.ReadCloser, error)

// LFSAuthenticate authenticates the client for LFS operations.
// func (s *Session) LFSAuthenticate(ctx context.Context, req *LFSAuthenticateRequest) (*LFSAuthenticateResponse, error)

// LFSTransfer uploads or downloads LFS objects.
// func (s *Session) LFSTransfer(ctx context.Context, req *LFSTransferRequest) (*LFSTransferResponse, error)
