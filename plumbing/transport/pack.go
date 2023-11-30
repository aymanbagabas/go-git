package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

// ServiceResponse contains the response from the server after a session handshake.
type ServiceResponse struct {
	// Version is the Git protocol version negotiated with the server.
	Version protocol.Version

	// Capabilities is the list of capabilities supported by the server.
	Capabilities *capability.List

	// AdvRefs is the list of references advertised by the server.
	// This is only populated if the server is using protocol v0 or v1.
	AdvRefs *packp.AdvRefs
}

// FetchRequest contains the parameters for a fetch request.
type FetchRequest struct {
	// Wants is the list of references to fetch.
	Wants []plumbing.Hash

	// Haves is the list of references the client already has.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool

	// Progress is the progress sideband.
	Progress sideband.Progress
}

// FetchResponse contains the response from the server after a fetch request.
type FetchResponse struct {
	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Shallows is the list of shallow references.
	Shallows []plumbing.Hash
}

// PushRequest contains the parameters for a push request.
type PushRequest struct {
	// UpdateRequests is the list of reference update requests.
	packp.UpdateRequests

	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Progress is the progress sideband.
	Progress sideband.Progress
}

// PushResponse contains the response from the server after a push request.
type PushResponse struct {
	// ReportStatus is the status of the reference update requests.
	packp.ReportStatus
}

// PackSession is a Git protocol transfer session.
// This is used by all protocols.
// TODO: rename this to Session.
type PackSession interface {
	io.Closer

	// Connection returns the underlying connection.
	Connection() (Connection, error)

	// Handshake starts the negotiation with the remote to get version if not
	// already connected.
	// Params are the optional extra parameters to be sent to the server. Use
	// this to send the protocol version of the client and any other extra parameters.
	Handshake(ctx context.Context, forPush bool, params ...string) (*ServiceResponse, error)

	// Supports returns whether the remote supports the given capability.
	Supports(c capability.Capability) bool

	// GetRemoteRefs returns the references advertised by the remote.
	// Using protocol v0 or v1, this returns the references advertised by the
	// remote during the handshake. Using protocol v2, this runs the ls-refs
	// command on the remote.
	// This will error if the session is not already established using
	// Handshake.
	GetRemoteRefs(ctx context.Context) (map[string]plumbing.Hash, error)

	// Fetch starts the negotiation with the remote to get references and packfiles.
	// This is associated with the git-upload-pack service.
	// This will error if the session is not already established using
	// Handshake.
	Fetch(ctx context.Context, req *FetchPackRequest) error

	// Push starts the negotiation with the remote to push references and packfiles.
	// This is associated with the git-receive-pack service.
	// This will error if the session is not already established using
	// Handshake.
	Push(ctx context.Context, req *PushRequest) (*PushResponse, error)
}

// Connectable is an interface that can be used to initiate a new Git service
// connection.
type Connectable interface {
	Connect(ctx context.Context, service string, ep *Endpoint, params ...string) (Connection, error)
}

// Connection is a full-duplex Git connection.
type Connection interface {
	io.ReadWriteCloser
}

// NewPackSession creates a new session that implements a full-duplex Git pack protocol.
func NewPackSession(c Connectable, s storage.Storer, ep *Endpoint) (PackSession, error) {
	ps := &packSession{
		st: s,
		ep: ep,
		c:  c,
	}
	return ps, nil
}

type packSession struct {
	st storage.Storer
	ep *Endpoint
	c  Connectable

	conn    Connection
	service string

	srvrsp *ServiceResponse
}

var _ PackSession = &packSession{}

// Connection implements Session.
func (p *packSession) Connection() (Connection, error) {
	if p.conn == nil {
		return nil, fmt.Errorf("connection not established")
	}

	return p.conn, nil
}

// Handshake implements Session.
func (p *packSession) Handshake(ctx context.Context, forPush bool, params ...string) (*ServiceResponse, error) {
	service := UploadPackServiceName
	if forPush {
		service = ReceivePackServiceName
	}

	var err error
	p.service = service
	p.conn, err = p.c.Connect(ctx, service, p.ep, params...)
	if err != nil {
		return nil, err
	}

	var remotever protocol.Version
	r := bufio.NewReader(p.conn)
	_, pkt, err := pktline.PeekPacketString(r)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(pkt, "version ") {
		v, _ := strconv.Atoi(pkt[8:])
		remotever = protocol.Version(v)
	}

	log.Printf("pktline: %q", pkt)

	ar := packp.NewAdvRefs()
	if err := ar.Decode(r); err != nil {
		return nil, err
	}

	// Some servers like jGit, announce capabilities instead of returning an
	// packp message with a flush. This verifies that we received a empty
	// adv-refs, even it contains capabilities.
	if !forPush && ar.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	FilterUnsupportedCapabilities(ar.Capabilities)

	resp := &ServiceResponse{
		Version:      remotever,
		Capabilities: ar.Capabilities,
		AdvRefs:      ar,
	}

	p.srvrsp = resp

	return resp, nil
}

// Supports implements Session.
func (p *packSession) Supports(c capability.Capability) bool {
	if p.srvrsp.AdvRefs == nil {
		return false
	}

	return p.srvrsp.AdvRefs.Capabilities.Supports(c)
}

// GetRemoteRefs implements Session.
func (p *packSession) GetRemoteRefs(ctx context.Context) (map[string]plumbing.Hash, error) {
	if p.srvrsp.AdvRefs == nil {
		// TODO: return appropriate error
		return nil, ErrEmptyRemoteRepository
	}

	refs := make(map[string]plumbing.Hash)
	for k, v := range p.srvrsp.AdvRefs.References {
		refs[k] = v
	}

	for k, v := range p.srvrsp.AdvRefs.Peeled {
		refs[k] = v
	}

	return refs, nil
}

// Fetch implements Session.
func (p *packSession) Fetch(ctx context.Context, req *FetchPackRequest) error {
	if p.conn == nil {
		return fmt.Errorf("connection not established")
	}

	if err := Negotiate(ctx, p.st, req, p, p.conn); err != nil {
		return err
	}

	return FetchPack(ctx, p.st, p, p.conn, req.Progress)
}

// Push implements Session.
func (*packSession) Push(ctx context.Context, req *PushRequest) (*PushResponse, error) {
	panic("unimplemented")
}

// Close implements Session.
func (*packSession) Close() error {
	return nil
}
