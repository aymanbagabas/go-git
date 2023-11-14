package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
)

// These are the services supported by go-git transports.
const (
	UploadPackServiceName  = "git-upload-pack"
	ReceivePackServiceName = "git-receive-pack"
)

// Transport can initiate git-upload-pack and git-receive-pack sessions to
// create fetch and push requests respectively.
type Transport interface {
	// NewSession starts a session for an endpoint and the given service.
	NewSession(service string, ep *Endpoint, auth AuthMethod) (Session, error)
}

// SessionOptions are the options for a session.
type SessionOptions struct {
	// TODO: add protocol v2 options
}

type Session interface {
	io.Closer

	// DiscoverReferences retrieves the advertised references for a
	// repository.
	// If the repository does not exist, returns ErrRepositoryNotFound.
	// If the repository exists, but is empty, returns ErrEmptyRemoteRepository.
	DiscoverReferences(ctx context.Context, forPush bool, opts *SessionOptions) (*packp.AdvRefs, error)

	// Fetch makes a git-upload-pack request and returns a response, including
	// a packfile.
	Fetch(ctx context.Context, req *packp.UploadPackRequest) (*packp.UploadPackResponse, error)

	// Push sends an git-receive-pack request and a packfile reader and returns
	// a ReportStatus and error.
	Push(ctx context.Context, req *packp.ReferenceUpdateRequest) (*packp.ReportStatus, error)
}
