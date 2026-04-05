package transport

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	oldtransport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// Git pack service command names.
const (
	UploadPackService  = "git-upload-pack"
	ReceivePackService = "git-receive-pack"
)

// PackClient creates pack protocol sessions on top of the transport Client.
type PackClient struct {
	client *Client
}

// NewPackClient creates a PackClient that uses the given transport Client.
func NewPackClient(client *Client) *PackClient {
	return &PackClient{client: client}
}

// Handshake opens a transport session for the given service, reads the
// advertised refs and capabilities, and returns a PackSession.
//
// The pack adapter builds the Request internally — callers provide the
// URL and service command name, and the adapter sets the correct command.
func (c *PackClient) Handshake(ctx context.Context, u *url.URL, service string, opts ...CallOption) (*PackSession, error) {
	req := &Request{
		URL:     u,
		Command: service,
	}

	sess, err := c.client.Open(ctx, req, opts...)
	if err != nil {
		return nil, err
	}

	r := bufio.NewReader(sess.Reader())
	w := sess.Writer()

	ver, err := oldtransport.DiscoverVersion(r)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}

	switch ver {
	case protocol.V2:
		_ = sess.Close()
		return nil, oldtransport.ErrUnsupportedVersion
	case protocol.V1, protocol.V0:
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(r); err != nil && !errors.Is(err, packp.ErrEmptyAdvRefs) {
		_ = sess.Close()
		return nil, err
	}

	return &PackSession{
		sess:    sess,
		r:       r,
		w:       w,
		svc:     service,
		version: ver,
		caps:    ar.Capabilities,
		refs:    ar,
	}, nil
}

// PackSession is a connected pack protocol session. It provides access
// to advertised refs and capabilities, and supports fetch and push
// operations over the underlying transport session.
type PackSession struct {
	sess    Session
	r       *bufio.Reader
	w       io.WriteCloser
	svc     string
	version protocol.Version
	caps    *capability.List
	refs    *packp.AdvRefs
}

// Capabilities returns the advertised server capabilities.
func (s *PackSession) Capabilities() *capability.List {
	return s.caps
}

// Version returns the discovered Git protocol version.
func (s *PackSession) Version() protocol.Version {
	return s.version
}

// GetRemoteRefs returns the references advertised by the remote.
func (s *PackSession) GetRemoteRefs(_ context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, oldtransport.ErrEmptyRemoteRepository
	}

	forPush := s.svc == ReceivePackService
	if !forPush && s.refs.IsEmpty() {
		return nil, oldtransport.ErrEmptyRemoteRepository
	}

	return s.refs.MakeReferenceSlice()
}

// Fetch sends a fetch-pack request to the server.
func (s *PackSession) Fetch(ctx context.Context, st storage.Storer, req *oldtransport.FetchRequest) error {
	shallows, err := negotiatePack(ctx, st, s.caps, false, s.r, s.w, req)
	if err != nil {
		return err
	}

	return fetchPack(ctx, st, s.caps, io.NopCloser(s.r), shallows, req)
}

// Push sends a send-pack request to the server.
func (s *PackSession) Push(ctx context.Context, st storage.Storer, req *oldtransport.PushRequest) error {
	return sendPack(ctx, s.caps, s.w, io.NopCloser(s.r), req)
}

// Close closes the underlying transport session.
func (s *PackSession) Close() error {
	return s.sess.Close()
}
