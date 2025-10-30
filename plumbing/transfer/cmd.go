package transfer

import (
	"context"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// DefaultProto is the default Git protocol version used by Go-Git client
// transfer.
const DefaultProto = protocol.V0

// Cmd is a Git command to be executed remotely.
type Cmd struct {
	// Service the Git service to be executed (e.g. git-upload-pack).
	Service string
	// Operation is an optional service operation to be executed.
	Operation string
	// URL the repository we're targeting.
	URL *url.URL
	// Proto the protocol version we're using (e.g. v0, v1, v2).
	Proto protocol.Version
	// AuthMethod the authentication method to be used.
	AuthMethod AuthMethod
}

// NewCommand creates a new [Cmd].
func NewCommand(service string, rawURL string) (*Cmd, error) {
	u, err := ParseURL(rawURL)
	if err != nil {
		return nil, err
	}

	return &Cmd{
		Service: service,
		URL:     u,
		Proto:   DefaultProto,
	}, nil
}

// Connector represents a transport mechanism that can connect to a Git remote.
type Connector interface {
	Connect(ctx context.Context, cmd *Cmd) (Conn, error)
}
