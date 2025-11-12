package transfer

import (
	"bufio"
	"context"
	"io"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// PackTransport is a transport that requires stateful connections.
type PackTransport struct {
	// Transport is the underlying [Connectable] transport used to connect to
	// remotes.
	Transport Connectable
}

var _ Transport = &PackTransport{}

// Connect implements [Connectable].
func (s *PackTransport) Connect(ctx context.Context, remoteURL *url.URL) (Runner, error) {
	if s.Transport == nil {
		panic("underlying Connectable transport is nil")
	}

	return s.Transport.Connect(ctx, remoteURL)
}

// Handshake implements [Transport].
func (t *PackTransport) Handshake(ctx context.Context, remoteURL *url.URL, cmd *Cmd) (Session, error) {
	var err error
	s := new(PackSession)

	s.rn, err = t.Connect(ctx, remoteURL)
	if err != nil {
		return nil, err
	}

	s.stdin, err = s.rn.StdinPipe()
	if err != nil {
		return nil, err
	}

	s.stdout, err = s.rn.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if sp, ok := s.rn.(StderrPiper); ok {
		s.stderr, err = sp.StderrPipe()
		if err != nil {
			return nil, err
		}
	}

	// We wrap the stdout with a context-aware reader to ensure that reads are
	// canceled when the context is done.
	s.r = bufio.NewReader(
		ioutil.NewContextReaderWithCloser(ctx, s.stdout, s.rn),
	)

	if err := s.rn.Start(ctx, cmd); err != nil {
		defer s.rn.Close() //nolint:errcheck
		return nil, stderrOrErr(s.stderr, err)
	}

	s.ver, err = DiscoverVersion(s.r)
	if err != nil {
		return nil, err
	}

	switch s.ver {
	case protocol.V2:
		return nil, ErrUnsupportedVersion
	case protocol.V1:
		// Read the version line
		fallthrough
	case protocol.V0:
		ar := packp.NewAdvRefs()
		if err := ar.Decode(s.r); err != nil {
			return nil, err
		}

		s.refs = ar
		s.caps = ar.Capabilities
	}

	return s, nil
}

// PackSession is a stateful, full-duplex established Git pack transfer
// [Session].
type PackSession struct {
	rn     Runner
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader

	// The reader for the stdout pipe
	r *bufio.Reader

	svc  string
	ver  protocol.Version
	caps *capability.List
	refs *packp.AdvRefs
}

var _ Session = &PackSession{}

// StatelessRPC implements Transport.
func (s *PackSession) StatelessRPC() bool {
	return false
}

// Capabilities implements Transport.
func (s *PackSession) Capabilities() *capability.List {
	return s.caps
}

// Version implements Transport.
func (s *PackSession) Version() protocol.Version {
	return s.ver
}

// GetRemoteRefs implements Transport.
func (s *PackSession) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	if s.r == nil {
		return nil, ErrNotEstablished
	}
	if s.refs == nil {
		// TODO: return appropriate error
		return nil, ErrEmptyRemoteRepository
	}

	// Some servers like jGit, announce capabilities instead of returning an
	// packp message with a flush. This verifies that we received a empty
	// adv-refs, even if it contains capabilities.
	forPush := s.svc == ServiceReceivePack
	if !forPush && s.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	return s.refs.MakeReferenceSlice()
}

// Fetch implements Transport.
func (s *PackSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	shallows, err := NegotiatePack(ctx, st, s, s.r, s.stdin, req)
	if err != nil {
		return err
	}

	return FetchPack(ctx, st, s, io.NopCloser(s.r), shallows, req)
}

// Push implements Transport.
func (s *PackSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	return SendPack(ctx, st, s, io.NopCloser(s.r), s.stdin, req)
}

// Close implements Transport.
func (s *PackSession) Close() error {
	_ = s.stdin.Close()
	return s.rn.Close()
}

// stderrOrErr reads from stderr and returns a [RemoteError] if there is any
// output. If there is no output, it returns the given err.
func stderrOrErr(stderr io.Reader, err error) error {
	if stderr != nil {
		if bts, rerr := io.ReadAll(stderr); rerr == nil && len(bts) > 0 {
			return &RemoteError{string(bts)}
		}
	}
	return err
}

func fetch(ctx context.Context, st storage.Storer, s Session, in io.Reader, out io.WriteCloser, req *FetchRequest) error {
	shallows, err := NegotiatePack(ctx, st, s, in, out, req)
	if err != nil {
		return err
	}

	return FetchPack(ctx, st, s, io.NopCloser(in), shallows, req)
}

func push(ctx context.Context, st storage.Storer, s Session, in io.Reader, out io.WriteCloser, req *PushRequest) error {
	return SendPack(ctx, st, s, io.NopCloser(in), out, req)
}
