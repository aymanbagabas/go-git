package transfer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/trace"
	"golang.org/x/net/proxy"
)

// DefaultHTTPTransport is the default HTTP transport used by go-git.
var DefaultHTTPTransport = &HTTPTransport{
	Client: &http.Client{
		Transport: http.DefaultTransport,
	},
	ForceDumb: false,
}

// HTTPTransport is a mechanism to transfer Git objects and references over
// HTTP/HTTPS protocols.
type HTTPTransport struct {
	// Client is the HTTP client used to perform requests.
	Client *http.Client

	// ForceDumb forces the use of the dumb HTTP protocol even if the server
	// advertises support for the smart HTTP protocol.
	ForceDumb bool
}

var (
	_ Transport            = &HTTPTransport{}
	_ ProxyURLConfigurer   = &HTTPTransport{}
	_ DialerConfigurer     = &HTTPTransport{}
	_ AuthMethodConfigurer = &HTTPTransport{}
	_ TLSConfigConfigurer  = &HTTPTransport{}
)

// ConfigureDialer configures a new copy of the transport to use a custom
// dialer.
// It implements the [DialerConfigurer] interface.
func (t *HTTPTransport) ConfigureDialer(dialer proxy.Dialer) (Transport, error) {
	t2 := t.Clone()
	if tr2, ok := t2.Client.Transport.(*http.Transport); ok {
		tr2 = tr2.Clone()
		tr2.Dial = dialer.Dial
		if d, ok := dialer.(proxy.ContextDialer); ok {
			tr2.DialContext = d.DialContext
		}
		t2.Client.Transport = tr2
		return t2, nil
	}
	return nil, fmt.Errorf("unable to configure dialer: transport is not http.Transport")
}

// ConfigureProxyURL configures the transport to use a proxy url.
// It implements the [ProxyURLConfigurer] interface.
func (t *HTTPTransport) ConfigureProxyURL(url *url.URL) (Transport, error) {
	t2 := t.Clone()
	if tr2, ok := t.Client.Transport.(*http.Transport); ok {
		tr2 = tr2.Clone()
		tr2.Proxy = http.ProxyURL(url)
		t2.Client.Transport = tr2
		return t2, nil
	}
	return nil, fmt.Errorf("unable to configure proxy url: transport is not http.Transport")
}

// ConfigureAuthMethod configures a new copy of the transport to use a custom
// auth method.
// It implements the [AuthMethodConfigurer] interface.
func (t *HTTPTransport) ConfigureAuthMethod(am AuthMethod) (Transport, error) {
	t2 := t.Clone()
	if err := am.SetAuth(t2); err != nil {
		return nil, err
	}
	return t2, nil
}

// ConfigureTLSConfig configures a new copy of the transport to use a custom
// TLS configuration.
// It implements the [TLSConfigConfigurer] interface.
func (t *HTTPTransport) ConfigureTLSConfig(cfg *tls.Config) (Transport, error) {
	t2 := t.Clone()
	if tr2, ok := t.Client.Transport.(*http.Transport); ok {
		tr2 = tr2.Clone()
		tr2.TLSClientConfig = cfg
		t2.Client.Transport = tr2
		return t2, nil
	}
	return nil, fmt.Errorf("unable to configure TLS config: transport is not http.Transport")
}

// Clone returns a copy of the transport.
func (t *HTTPTransport) Clone() *HTTPTransport {
	if t.Client == nil {
		t.Client = &http.Client{}
	}
	tr := t.Client.Transport
	if tr2, ok := tr.(*http.Transport); ok {
		tr = tr2.Clone()
	}
	t2 := &HTTPTransport{
		Client: &http.Client{
			Transport:     tr,
			CheckRedirect: t.Client.CheckRedirect,
			Jar:           t.Client.Jar,
			Timeout:       t.Client.Timeout,
		},
		ForceDumb: t.ForceDumb,
	}
	return t2
}

// RoundTrip implements the [http.RoundTripper] interface.
// It delegates to the underlying Transport if set, or to
// http.DefaultTransport otherwise.
func (t *HTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Client == nil {
		t.Client = &http.Client{}
	}
	if t.Client.Transport == nil {
		t.Client.Transport = http.DefaultTransport
	}
	return t.Client.Transport.RoundTrip(req)
}

// Connect connects to a Git repository over HTTP/HTTPS.
func (t *HTTPTransport) Connect(ctx context.Context, cmd *Cmd) (net.Conn, error) {
	if t.Client == nil {
		t.Client = &http.Client{}
	}
	if t.Client.Transport == nil {
		t.Client.Transport = http.DefaultTransport
	}

	client := &http.Client{
		Transport:     t.Client.Transport,
		CheckRedirect: t.Client.CheckRedirect,
		Jar:           t.Client.Jar,
		Timeout:       t.Client.Timeout,
	}

	var proto string
	if cmd.Proto > protocol.V0 {
		proto = protocol.FormatVersion(cmd.Proto)
	}

	u := *cmd.URL
	endpoint, err := url.JoinPath(u.String(), infoRefsPath)
	if err != nil {
		return nil, err
	}

	if !t.ForceDumb {
		endpoint += "?service=" + cmd.Service
	}

	svc := cmd.Service
	c := &HTTPConn{
		client: client,
		url:    &u,
		svc:    svc,
		proto:  proto,
	}

	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	c.req = req

	applyHeaders(req, cmd.Service, &u, proto, !t.ForceDumb)
	res, err := doRequest(client, req)
	if err != nil {
		return nil, err
	}

	c.res = res
	defer res.Body.Close() //nolint:errcheck

	if !t.ForceDumb {
		// Determine if the server is using the smart protocol
		contentType := res.Header.Get("Content-Type")
		expectedContentType := fmt.Sprintf("application/x-%s-advertisement", cmd.Service)
		c.isSmart = strings.Contains(contentType, expectedContentType)
	}

	modifyRedirect(res, &u)

	rd := bufio.NewReader(res.Body)
	ar := packp.NewAdvRefs()
	if c.isSmart {
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

			if reply.Service != svc {
				return nil, fmt.Errorf("unexpected service name: %w", transport.ErrInvalidResponse)
			}
		}

		c.ver, _ = transport.DiscoverVersion(rd)
		switch c.ver {
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

		walker := newFetchWalker(c, ctx, nil)
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

	c.refs = ar

	return c, nil
}

// HTTPSession represents a session for a Git commands over HTTP/HTTPS.
type HTTPSession struct {
	c *HTTPConn
}

var _ Session = &HTTPSession{}

// NewSession creates a new session for the given command.
func (t *HTTPTransport) NewSession(ctx context.Context, cmd *Cmd) (Session, error) {
	conn, err := t.Connect(ctx, cmd)
	if err != nil {
		return nil, err
	}

	httpConn, ok := conn.(*HTTPConn)
	if !ok {
		panic("expected *HTTPConn from Connect")
	}

	s := &HTTPSession{
		c: httpConn,
	}

	return s, nil
}

// Capabilities implements Session.
func (s *HTTPSession) Capabilities() *capability.List {
	return s.c.refs.Capabilities
}

// Close implements Session.
func (s *HTTPSession) Close() error {
	return s.c.Close()
}

// Fetch implements Session.
func (s *HTTPSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	if !s.c.isSmart {
		return s.c.fetchDumb(ctx, st, req)
	}

	// Set the context for the connection requests.
	s.c.ctx = ctx

	// XXX: packfile will be populated and accessible once rwc.Close() is
	// called in NegotiatePack.
	shallows, err := NegotiatePack(ctx, st, s, s.c, s.c, req)
	if err != nil {
		if s.c.res != nil {
			// Make sure the response body is closed.
			defer s.c.Close() // nolint: errcheck
		}
		return err
	}

	return FetchPack(ctx, st, s, s.c, shallows, req)
}

// GetRemoteRefs implements Session.
func (s *HTTPSession) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	if s.c.refs == nil {
		return nil, transport.ErrEmptyRemoteRepository
	}

	// Git 2.41+ returns a zero-id plus capabilities when an empty
	// repository is being cloned. This skips the existing logic within
	// advrefs_decode.decodeFirstHash, which expects a flush-pkt instead.
	//
	// This logic aligns with plumbing/transport/common/common.go.
	forPush := s.c.svc == ServiceReceivePack
	if s.c.refs.IsEmpty() && !forPush {
		// Empty repositories are valid for git-receive-pack.
		return nil, transport.ErrEmptyRemoteRepository
	}

	return s.c.refs.MakeReferenceSlice()
}

// Push implements Session.
func (s *HTTPSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	// Set the context for the connection requests.
	s.c.ctx = ctx
	return SendPack(ctx, st, s, s.c, s.c, req)
}

// StatelessRPC implements Session.
func (s *HTTPSession) StatelessRPC() bool {
	return true
}

// Version implements Session.
func (s *HTTPSession) Version() protocol.Version {
	return s.c.ver
}

// HTTPConn is a connection to a Git repository over HTTP/HTTPS.
type HTTPConn struct {
	ctx     context.Context
	client  *http.Client
	url     *url.URL
	svc     string
	proto   string
	isSmart bool // whether the connection is using the smart protocol
	ver     protocol.Version
	refs    *packp.AdvRefs // the advertised references

	reqBuf bytes.Buffer
	req    *http.Request  // the last request made
	res    *http.Response // the last response received
	conn   net.Conn       // the underlying TCP network connection
}

// Read reads data from the connection.
func (c *HTTPConn) Read(p []byte) (n int, err error) {
	if c.res == nil {
		panic("http: requester.Read called before requester.Close")
	}
	return c.res.Body.Read(p)
}

// Write writes data to the connection.
func (c *HTTPConn) Write(p []byte) (n int, err error) {
	return c.reqBuf.Write(p)
}

// Close closes the connection.
func (c *HTTPConn) Close() error {
	defer c.reqBuf.Reset()

	if c.res != nil {
		_ = c.res.Body.Close()
	}

	var err error
	method := http.MethodPost
	urlStr, err := url.JoinPath(c.url.String(), c.svc)
	if err != nil {
		return err
	}

	c.req, err = c.newRequest(c.ctx, method, urlStr, &c.reqBuf)
	if err != nil {
		return err
	}

	applyHeaders(c.req, c.svc, c.url, c.proto, c.isSmart)
	c.res, err = doRequest(c.client, c.req)
	if err != nil {
		return err
	}

	return nil
}

// LocalAddr returns the local address of the connection.
func (c *HTTPConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address of the connection.
func (c *HTTPConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *HTTPConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *HTTPConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *HTTPConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *HTTPConn) newRequest(
	ctx context.Context,
	method, urlStr string,
	body io.Reader,
) (*http.Request, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			c.conn = info.Conn
		},
	}

	ctx = httptrace.WithClientTrace(ctx, trace)
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, err
	}

	return req, nil
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
