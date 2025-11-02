package transfer

import (
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
	// Proto the protocol version we're requesting (e.g. v0, v1, v2).
	Proto protocol.Version
}

// Command returns a new [Cmd] instance for the given service and repository
// URL.
func Command(svc string, u *url.URL) *Cmd {
	return &Cmd{
		Service: svc,
		URL:     u,
		Proto:   DefaultProto,
	}
}
