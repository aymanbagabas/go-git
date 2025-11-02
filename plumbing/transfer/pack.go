package transfer

import (
	"bufio"
	"context"
	"io"
	"net"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// PackSession represents a Git pack transfer session.
type PackSession struct {
	// the service being used (e.g., "git-upload-pack" or "git-receive-pack")
	svc  string
	conn net.Conn
	r    *bufio.Reader
	w    io.WriteCloser

	ver  protocol.Version
	caps *capability.List
	refs *packp.AdvRefs
}

var _ Session = &PackSession{}

// NewPackSession creates a new [PackSession] based on the given [net.Conn] and
// [Cmd].
func NewPackSession(ctx context.Context, conn net.Conn, cmd *Cmd) (*PackSession, error) {
	svc := cmd.Service
	switch svc {
	case ServiceUploadPack, ServiceReceivePack:
		// do nothing
	default:
		return nil, ErrUnsupportedService
	}

	ps := &PackSession{
		svc:  svc,
		conn: conn,
		w:    conn,
	}

	cr := ioutil.NewContextReaderWithCloser(ctx, conn, ps)
	ps.r = bufio.NewReader(cr)

	var err error
	ps.ver, err = DiscoverVersion(ps.r)
	if err != nil {
		return nil, err
	}

	switch ps.ver {
	case protocol.V2:
		return nil, ErrUnsupportedVersion
	case protocol.V1:
		// Read the version line
		fallthrough
	case protocol.V0:
		ar := packp.NewAdvRefs()
		if err := ar.Decode(ps.r); err != nil {
			return nil, err
		}

		ps.refs = ar
		ps.caps = ar.Capabilities
	}

	return ps, nil
}

// StatelessRPC reports whether the session is stateless.
func (p *PackSession) StatelessRPC() bool {
	return false
}

// Capabilities returns the capabilities supported by the remote side.
func (p *PackSession) Capabilities() *capability.List {
	return p.caps
}

// Close implements Session.
func (p *PackSession) Close() error {
	return p.conn.Close()
}

// Fetch implements Session.
func (p *PackSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	return fetch(ctx, st, p, p.r, p.w, req)
}

// GetRemoteRefs implements Session.
func (p *PackSession) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	if p.refs == nil {
		// TODO: return appropriate error
		return nil, ErrEmptyRemoteRepository
	}

	// Some servers like jGit, announce capabilities instead of returning an
	// packp message with a flush. This verifies that we received a empty
	// adv-refs, even if it contains capabilities.
	forPush := p.svc == ServiceReceivePack
	if !forPush && p.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	return p.refs.MakeReferenceSlice()
}

// Push implements Session.
func (p *PackSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	return push(ctx, st, p, p.w, p.r, req)
}

// Version implements Session.
func (p *PackSession) Version() protocol.Version {
	return p.ver
}

func fetch(ctx context.Context, st storage.Storer, s Session, in io.Reader, out io.WriteCloser, req *FetchRequest) error {
	shallows, err := NegotiatePack(ctx, st, s, in, out, req)
	if err != nil {
		return err
	}

	return FetchPack(ctx, st, s, io.NopCloser(in), shallows, req)
}

func push(ctx context.Context, st storage.Storer, s Session, out io.WriteCloser, in io.Reader, req *PushRequest) error {
	return SendPack(ctx, st, s, out, io.NopCloser(in), req)
}
