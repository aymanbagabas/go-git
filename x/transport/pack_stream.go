package transport

import (
	"bufio"
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
)

// StreamPackSession implements PackSession over a full-duplex stream.
// Stream transports (SSH, Git TCP, file) call NewStreamPackSession from
// their Handshake implementation.
type StreamPackSession struct {
	sess    Session
	r       *bufio.Reader
	w       io.WriteCloser
	svc     string
	version protocol.Version
	caps    *capability.List
	refs    *packp.AdvRefs
}

// NewStreamPackSession reads version + adv-refs from the session and
// returns a ready StreamPackSession.
func NewStreamPackSession(sess Session, service string) (*StreamPackSession, error) {
	r := bufio.NewReader(sess.Reader())
	w := sess.Writer()

	ver, err := DiscoverVersion(r)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}

	switch ver {
	case protocol.V2:
		_ = sess.Close()
		return nil, ErrUnsupportedVersion
	case protocol.V1, protocol.V0:
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(r); err != nil && !errors.Is(err, packp.ErrEmptyAdvRefs) {
		_ = sess.Close()
		return nil, err
	}

	return &StreamPackSession{
		sess:    sess,
		r:       r,
		w:       w,
		svc:     service,
		version: ver,
		caps:    ar.Capabilities,
		refs:    ar,
	}, nil
}

// Capabilities implements PackSession.
func (s *StreamPackSession) Capabilities() *capability.List { return s.caps }

// GetRemoteRefs implements PackSession.
func (s *StreamPackSession) GetRemoteRefs(_ context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, ErrEmptyRemoteRepository
	}
	forPush := s.svc == ReceivePackService
	if !forPush && s.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}
	return s.refs.MakeReferenceSlice()
}

// Fetch implements PackSession.
func (s *StreamPackSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	shallows, err := NegotiatePack(ctx, st, s.caps, false, s.r, s.w, req)
	if err != nil {
		return err
	}
	return FetchPack(ctx, st, s.caps, io.NopCloser(s.r), shallows, req)
}

// Push implements PackSession.
func (s *StreamPackSession) Push(ctx context.Context, _ storage.Storer, req *PushRequest) error {
	return SendPack(ctx, s.caps, s.w, io.NopCloser(s.r), req)
}

// Close implements PackSession.
func (s *StreamPackSession) Close() error { return s.sess.Close() }

var _ PackSession = (*StreamPackSession)(nil)
