package transport

import (
	"context"
	"io"
	"net"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
)

// DialContextFunc is the function signature for dialing network connections.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

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

	// Quiet indicates whether the server should suppress human-readable
	// output.
	Quiet bool
}
