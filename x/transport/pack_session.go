package transport

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
)

// PackTransport is implemented by transports that speak the Git pack
// protocol. Each transport implements this directly — stream transports
// use the NewStreamPackSession helper, HTTP handles smart/dumb internally.
type PackTransport interface {
	Handshake(ctx context.Context, req *Request) (PackSession, error)
}

// PackSession is returned by PackTransport.Handshake.
type PackSession interface {
	Capabilities() *capability.List
	GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)
	Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error
	Push(ctx context.Context, st storage.Storer, req *PushRequest) error
	Close() error
}
