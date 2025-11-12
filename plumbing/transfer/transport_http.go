package transfer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// DefaultHTTPTransport is the default HTTP transport used by go-git.
var DefaultHTTPTransport = &HTTPClient{
	Client: http.Client{
		Transport: http.DefaultTransport,
	},
	ForceDumb: false,
}

// HTTPClient is a mechanism to transfer Git objects and references over
// HTTP/HTTPS protocols.
type HTTPClient struct {
	// Client is the HTTP client used to perform requests.
	http.Client

	// ForceDumb forces the use of the dumb HTTP protocol even if the server
	// advertises support for the smart HTTP protocol.
	ForceDumb bool

	// AuthCallback is a callback function that will be called before doing any
	// request, to set authentication headers and other auth-related settings.
	AuthCallback func(*http.Request)
}

var _ Transport = &HTTPTransport{}

// HTTPTransport is a transport layer over HTTP/HTTPS protocols that can
// establish Git pack transfer [Session]s.
type HTTPTransport struct {
	// Client is the HTTP client used to perform requests.
	http.Client

	// ForceDumb forces the use of the dumb HTTP protocol even if the server
	// advertises support for the smart HTTP protocol.
	ForceDumb bool

	// AuthCallback is a callback function that will be called before doing any
	// request, to set authentication headers and other auth-related settings.
	AuthCallback func(*http.Request)
}

// Handshake implements Transport.
func (c *HTTPTransport) Handshake(ctx context.Context, remoteURL *url.URL, cmd *Cmd) (Session, error) {
	if c.Client.Transport == nil {
		c.Client.Transport = http.DefaultTransport
	}

	var proto string
	if cmd.Proto > protocol.V0 {
		proto = protocol.FormatVersion(cmd.Proto)
	}

	s := new(HTTPSession)
	s.svc = cmd.Service
	s.authCb = c.AuthCallback
	s.proto = proto
	s.url = remoteURL
	s.client = &c.Client

	u := *remoteURL
	endpoint, err := url.JoinPath(u.String(), infoRefsPath)
	if err != nil {
		return nil, err
	}

	if !c.ForceDumb {
		endpoint += "?service=" + cmd.Service
	}

	req, err := newRequest(ctx, http.MethodGet, endpoint, nil, c.AuthCallback)
	if err != nil {
		return nil, err
	}

	applyHeaders(req, cmd.Service, &u, proto, !c.ForceDumb)
	res, err := doRequest(&c.Client, req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close() //nolint:errcheck

	if !c.ForceDumb {
		// Determine if the server is using the smart protocol
		contentType := res.Header.Get("Content-Type")
		expectedContentType := fmt.Sprintf("application/x-%s-advertisement", cmd.Service)
		s.isSmart = strings.Contains(contentType, expectedContentType)
	}

	modifyRedirect(res, &u)

	rd := bufio.NewReader(res.Body)
	ar := packp.NewAdvRefs()
	if s.isSmart {
		_, prefix, err := pktline.PeekLine(rd)
		if err != nil {
			return nil, err
		}

		// Consumes the prefix
		//  # service=<service>\n
		//  0000
		if bytes.HasPrefix(prefix, []byte("# service=")) {
			var reply packp.SmartReply
			err := reply.Decode(rd)
			if err != nil {
				return nil, err
			}

			if reply.Service != s.svc {
				return nil, fmt.Errorf("unexpected service name: %w", transport.ErrInvalidResponse)
			}
		}

		s.ver, _ = transport.DiscoverVersion(rd)
		switch s.ver {
		case protocol.V2:
			return nil, transport.ErrUnsupportedVersion
		case protocol.V1:
			// Read the version line
			fallthrough
		case protocol.V0:
		}

		if err = ar.Decode(rd); err != nil {
			if err == packp.ErrEmptyAdvRefs {
				err = transport.ErrEmptyRemoteRepository
			}

			return nil, err
		}
	} else {
		var infoRefs packp.InfoRefs
		if err := infoRefs.Decode(rd); err != nil {
			return nil, err
		}

		ar.References = infoRefs.References
		ar.Peeled = infoRefs.Peeled

		walker := newFetchWalker(s, ctx, nil)
		head, err := walker.getHead()
		if err != nil {
			return nil, err
		}

		var hash plumbing.Hash
		switch head.Type() {
		case plumbing.SymbolicReference:
			for name, refHash := range ar.References {
				if name == head.Target().String() {
					hash = refHash
					break
				}
			}
		default:
			hash = head.Hash()
		}
		ar.Head = &hash
	}

	s.refs = ar

	return s, nil
}

// HTTPSession is a stateless established Git pack transfer [Session] over
// HTTP/HTTPS.
type HTTPSession struct {
	isSmart bool // whether the connection is using the smart protocol
	ver     protocol.Version
	svc     string
	refs    *packp.AdvRefs
	authCb  func(*http.Request)
	proto   string   // the Git-Protocol header value
	url     *url.URL // the endpoint remote URL
	client  *http.Client
}

var _ Session = &HTTPSession{}

// Capabilities implements Session.
func (s *HTTPSession) Capabilities() *capability.List {
	return s.refs.Capabilities
}

// Close implements Session.
func (s *HTTPSession) Close() error {
	return nil
}

// Fetch implements Session.
func (s *HTTPSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	if !s.isSmart {
		return s.fetchDumb(ctx, st, req)
	}

	rwc := newRequester(ctx, s)

	// XXX: packfile will be populated and accessible once rwc.Close() is
	// called in NegotiatePack.
	packfile := rwc.BodyCloser()
	shallows, err := NegotiatePack(ctx, st, s, packfile, rwc, req)
	if err != nil {
		if rwc.res != nil {
			// Make sure the response body is closed.
			defer packfile.Close() // nolint: errcheck
		}
		return err
	}

	return FetchPack(ctx, st, s, packfile, shallows, req)
}

// GetRemoteRefs implements Session.
func (s *HTTPSession) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, transport.ErrEmptyRemoteRepository
	}

	// Git 2.41+ returns a zero-id plus capabilities when an empty
	// repository is being cloned. This skips the existing logic within
	// advrefs_decode.decodeFirstHash, which expects a flush-pkt instead.
	//
	// This logic aligns with plumbing/transport/common/common.go.
	forPush := s.svc == ServiceReceivePack
	if s.refs.IsEmpty() && !forPush {
		// Empty repositories are valid for git-receive-pack.
		return nil, transport.ErrEmptyRemoteRepository
	}

	return s.refs.MakeReferenceSlice()
}

// Push implements Session.
func (s *HTTPSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	rwc := newRequester(ctx, s)
	return SendPack(ctx, st, s, rwc.BodyCloser(), rwc, req)
}

// StatelessRPC implements Session.
func (s *HTTPSession) StatelessRPC() bool {
	return true
}

// Version implements Session.
func (s *HTTPSession) Version() protocol.Version {
	return s.ver
}

// requester is a io.WriteCloser that sends an HTTP request to on close and
// reads the response into the struct.
type requester struct {
	*HTTPSession
	ctx    context.Context
	reqBuf bytes.Buffer
	req    *http.Request  // the last request made
	res    *http.Response // the last response received
	url    *url.URL
}

func newRequester(ctx context.Context, s *HTTPSession) *requester {
	return &requester{
		HTTPSession: s,
		ctx:         ctx,
	}
}

var _ io.ReadWriteCloser = &requester{}

// BodyCloser returns the response body as an io.ReadCloser.
func (r *requester) BodyCloser() io.ReadCloser {
	return ioutil.NewReadCloser(r, ioutil.CloserFunc(func() error {
		if r.res == nil {
			panic("http: requester.res is accessed before requester.Close")
		}
		return r.res.Body.Close()
	}))
}

// Read implements io.ReadWriteCloser.
func (r *requester) Read(p []byte) (n int, err error) {
	if r.res == nil {
		panic("http: requester.Read called before requester.Close")
	}
	return r.res.Body.Read(p)
}

// Close implements io.ReadWriteCloser.
func (r *requester) Close() (err error) {
	defer r.reqBuf.Reset()

	if r.res != nil {
		_ = r.res.Body.Close()
	}

	method := http.MethodPost
	urlStr, err := url.JoinPath(r.url.String(), r.svc)
	if err != nil {
		return err
	}

	r.req, err = newRequest(r.ctx, method, urlStr, &r.reqBuf, r.authCb)
	if err != nil {
		return err
	}

	applyHeaders(r.req, r.svc, r.url, r.proto, r.isSmart)
	r.res, err = doRequest(r.client, r.req)
	if err != nil {
		return err
	}

	return nil
}

func newRequest(
	ctx context.Context,
	method, urlStr string,
	body io.Reader,
	authCb func(*http.Request),
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, err
	}

	if authCb != nil {
		authCb(req)
	}

	return req, nil
}

// Write implements io.ReadWriteCloser.
func (r *requester) Write(p []byte) (n int, err error) {
	return r.reqBuf.Write(p)
}

// HTTPError is a dedicated error to return errors based on http status code.
type HTTPError struct {
	URL    *url.URL
	Status int
	Reason string
}

// StatusCode returns the status code of the response
func (e *HTTPError) StatusCode() int {
	return e.Status
}

func (e *HTTPError) Error() string {
	format := "unexpected requesting %q status code: %d"
	if e.Reason != "" {
		return fmt.Sprintf(format+": %s", e.URL, e.Status, e.Reason)
	}
	return fmt.Sprintf(format, e.URL, e.Status)
}

const infoRefsPath = "/info/refs"

// modifyRedirect modifies the endpoint based on the redirect response.
func modifyRedirect(res *http.Response, ep *url.URL) {
	if res.Request == nil {
		return
	}

	r := res.Request
	if !strings.HasSuffix(r.URL.Path, infoRefsPath) {
		return
	}

	ep.Host = r.URL.Host
	ep.Scheme = r.URL.Scheme
	ep.Path = r.URL.Path[:len(r.URL.Path)-len(infoRefsPath)]
}

func doRequest(
	client *http.Client,
	req *http.Request,
) (*http.Response, error) {
	traceHTTP := trace.HTTP.Enabled()
	if traceHTTP {
		trace.HTTP.Printf("requesting %s %s %v", req.Method, req.URL.String(), req.Header)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if traceHTTP {
		trace.HTTP.Printf("response %s %s %s %v", res.Proto, res.Status, res.Request.URL.String(), res.Header)
	}

	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return res, nil
	}

	return res, checkError(res)
}

// checkError returns a new Err based on a http response.
func checkError(r *http.Response) error {
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var reason string

	// If a response message is present, add it to error
	var messageBuffer bytes.Buffer
	if r.Body != nil {
		messageLength, _ := messageBuffer.ReadFrom(r.Body)
		if messageLength > 0 {
			reason = messageBuffer.String()
		}
	}

	switch r.StatusCode {
	case http.StatusUnauthorized:
		return transport.ErrAuthenticationRequired
	case http.StatusForbidden:
		return transport.ErrAuthorizationFailed
	case http.StatusNotFound:
		return transport.ErrRepositoryNotFound
	}

	return plumbing.NewUnexpectedError(&HTTPError{
		URL:    r.Request.URL,
		Status: r.StatusCode,
		Reason: reason,
	})
}

func applyHeaders(
	req *http.Request,
	service string,
	ep *url.URL,
	protocol string,
	useSmart bool,
) {
	// Add headers
	req.Header.Set("User-Agent", capability.DefaultAgent())
	req.Header.Set("Host", ep.Host) // host:port

	if useSmart {
		req.Header.Set("Content-Type", fmt.Sprintf("application/x-%s-request", service))
		req.Header.Set("Accept", fmt.Sprintf("application/x-%s-result", service)) // smart protocol
	}

	if protocol != "" {
		req.Header.Set("Git-Protocol", protocol)
	}
}
