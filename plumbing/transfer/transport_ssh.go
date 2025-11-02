package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"time"

	sshinternal "github.com/go-git/go-git/v6/internal/ssh"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
	"github.com/kevinburke/ssh_config"
	sshagent "github.com/xanzy/ssh-agent"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

// DefaultSSHPort is the default port for SSH connections.
const DefaultSSHPort = "22"

// DefaultSSHConfig is the reader used to access parameters stored in the
// system's ssh_config files. If nil all the ssh_config are ignored.
var DefaultSSHConfig = ssh_config.DefaultUserSettings

// DefaultSSHTransport is the default SSH transport.
var DefaultSSHTransport = &SSHTransport{
	ClientConfig: &ssh.ClientConfig{},
	Dialer:       proxy.FromEnvironmentUsing(DefaultDialer),
}

// SSHTransport is a mechanism for SSH Git transports.
type SSHTransport struct {
	ClientConfig *ssh.ClientConfig
	Dialer       proxy.Dialer
}

var (
	_ Transport            = &SSHTransport{}
	_ ProxyURLConfigurer   = &SSHTransport{}
	_ DialerConfigurer     = &SSHTransport{}
	_ AuthMethodConfigurer = &SSHTransport{}
)

// ConfigureDialer configures a new copy of the transport with the given
// dialer.
// It implements the [DialerConfigurer] interface.
func (t *SSHTransport) ConfigureDialer(dialer proxy.Dialer) (Transport, error) {
	t2 := t.Clone()
	t2.Dialer = dialer
	return t2, nil
}

// ConfigureProxyURL configures a new copy of the transport with the given
// proxy URL.
// It implements the [ProxyURLConfigurer] interface.
func (t *SSHTransport) ConfigureProxyURL(proxyURL *url.URL) (Transport, error) {
	t2 := t.Clone()
	dialer, err := proxy.FromURL(proxyURL, DefaultDialer)
	if err != nil {
		return nil, fmt.Errorf("failed to configure proxy URL: %w", err)
	}
	t2.Dialer = dialer
	return t2, nil
}

// ConfigureAuthMethod configures a new copy of the transport with the given
// authentication method.
// It implements the [AuthMethodConfigurer] interface.
func (t *SSHTransport) ConfigureAuthMethod(auth AuthMethod) (Transport, error) {
	t2 := t.Clone()
	if err := auth.SetAuth(t2); err != nil {
		return nil, fmt.Errorf("failed to configure auth method: %w", err)
	}
	return t2, nil
}

// Clone returns a new copy of the transport.
func (t *SSHTransport) Clone() *SSHTransport {
	var cc ssh.ClientConfig
	if t.ClientConfig != nil {
		cc = *t.ClientConfig
	}

	return &SSHTransport{
		ClientConfig: &cc,
		Dialer:       t.Dialer,
	}
}

// Connect connects to a remote Git repository over SSH.
func (t *SSHTransport) Connect(ctx context.Context, cmd *Cmd) (net.Conn, error) {
	var cc ssh.ClientConfig
	if t.ClientConfig != nil {
		cc = *t.ClientConfig
	}
	if t.Dialer == nil {
		t.Dialer = &net.Dialer{}
	}

	username := urlUsernameOrLocal(cmd.URL)
	setDefaultAuth(&cc, username)

	hostWithPort := getHostWithPort(cmd.URL)
	if cc.HostKeyCallback == nil {
		db, err := sshinternal.NewKnownHostsDB()
		if err != nil {
			return nil, err
		}

		cc.HostKeyCallback = db.HostKeyCallback()
		cc.HostKeyAlgorithms = db.HostKeyAlgorithms(hostWithPort)
	} else if len(cc.HostKeyAlgorithms) == 0 {
		// Set the HostKeyAlgorithms based on HostKeyCallback.
		// For background see https://github.com/go-git/go-git/issues/411 as well as
		// https://github.com/golang/go/issues/29286 for root cause.
		db, err := sshinternal.NewKnownHostsDB()
		if err != nil {
			return nil, err
		}

		// Note that the knownhost database is used, as it provides additional functionality
		// to handle ssh cert-authorities.
		cc.HostKeyAlgorithms = db.HostKeyAlgorithms(hostWithPort)
	}

	var cancel context.CancelFunc
	if cc.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, cc.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	defer cancel()

	var conn net.Conn
	var err error
	if d, ok := t.Dialer.(proxy.ContextDialer); ok {
		conn, err = d.DialContext(ctx, "tcp", hostWithPort)
	} else {
		// Context is ignored
		conn, err = t.Dialer.Dial("tcp", hostWithPort)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to dial SSH connection: %w", err)
	}

	sconn, chans, reqs, err := ssh.NewClientConn(conn, hostWithPort, &cc)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH client connection: %w", err)
	}

	c := ssh.NewClient(sconn, chans, reqs)
	s, err := c.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}

	sc, err := newSSHConn(conn, c, s)
	if err != nil {
		_ = s.Close()
		_ = c.Close()
		return nil, fmt.Errorf("failed to create SSH connection: %w", err)
	}

	// Capture stderr output for error reporting
	var stderrBuf bytes.Buffer
	go io.Copy(&stderrBuf, sc.stderr) //nolint:errcheck

	if cmd.Proto > protocol.V0 {
		if err := s.Setenv("GIT_PROTOCOL", protocol.FormatVersion(cmd.Proto)); err != nil {
			_ = sc.Close()
			return nil, fmt.Errorf("failed to set GIT_PROTOCOL env: %w", err)
		}
	}
	if err := s.Start(buildSSHCommand(cmd)); err != nil {
		_ = sc.Close()
		if stderrBuf.Len() > 0 {
			return nil, &RemoteError{stderrBuf.String()}
		}
		return nil, fmt.Errorf("failed to start SSH session: %w", err)
	}

	return sc, nil
}

// SSHSession is a Git transport session over SSH.
type SSHSession struct {
	*PackSession
	c *SSHConn
}

// Fetch implements Session.
func (s *SSHSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	// We need to use an [io.WriteCloser] that closes the stdin of the SSH
	// session to signal the end of the request.
	wc := ioutil.NewWriteCloser(s.c.stdin, s.c.stdin)
	return fetch(ctx, st, s.PackSession, s.c.stdout, wc, req)
}

// Push implements Session.
func (s *SSHSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	// We need to use an [io.WriteCloser] that closes the stdin of the SSH
	// session to signal the end of the request.
	wc := ioutil.NewWriteCloser(s.c.stdin, s.c.stdin)
	return push(ctx, st, s.PackSession, wc, s.c.stdout, req)
}

// NewSession starts a new [Session] over SSH.
func (t *SSHTransport) NewSession(ctx context.Context, cmd *Cmd) (Session, error) {
	c, err := t.Connect(ctx, cmd)
	if err != nil {
		return nil, err
	}

	s, err := NewPackSession(ctx, c, cmd)
	if err != nil {
		_ = c.Close()
		return nil, err
	}

	return &SSHSession{
		PackSession: s,
		c:           c.(*SSHConn),
	}, nil
}

// SSHConn is a Git transport connection over SSH.
type SSHConn struct {
	conn   net.Conn
	c      *ssh.Client
	s      *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

func newSSHConn(conn net.Conn, c *ssh.Client, s *ssh.Session) (*SSHConn, error) {
	sc := &SSHConn{conn: conn, c: c, s: s}
	stdin, err := s.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := s.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := s.StderrPipe()
	if err != nil {
		return nil, err
	}

	sc.stdin = stdin
	sc.stdout = stdout
	sc.stderr = stderr

	return sc, nil
}

// Read reads from the SSH connection.
func (c *SSHConn) Read(p []byte) (n int, err error) {
	return c.stdout.Read(p)
}

// Write writes to the SSH connection.
func (c *SSHConn) Write(p []byte) (n int, err error) {
	return c.stdin.Write(p)
}

// Close closes the SSH connection.
func (c *SSHConn) Close() (rErr error) {
	// XXX: If did read the full packfile, then the session might be already
	//     closed.
	_ = c.s.Close()
	err := c.c.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}

// LocalAddr returns the local address of the SSH connection.
func (c *SSHConn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

// RemoteAddr returns the remote address of the SSH connection.
func (c *SSHConn) RemoteAddr() net.Addr {
	return c.c.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated with the
// connection.
func (c *SSHConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *SSHConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *SSHConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func buildSSHCommand(cmd *Cmd) string {
	s := fmt.Sprintf("%s '%s'", cmd.Service, cmd.URL.Path)
	if cmd.Operation != "" {
		s += " " + cmd.Operation
	}
	return s
}

func tracePublicKeysCallback(getSigners func() ([]ssh.Signer, error)) ssh.AuthMethod {
	signers, err := getSigners()
	if err != nil {
		trace.SSH.Printf("ssh: error calling getSigners: %v", err)
	}
	if len(signers) == 0 {
		trace.SSH.Printf("ssh: no signers found")
	}
	for _, s := range signers {
		trace.SSH.Printf("ssh: found key: %s %s", s.PublicKey().Type(),
			ssh.FingerprintSHA256(s.PublicKey()))
	}

	cb := func() ([]ssh.Signer, error) {
		return signers, err
	}
	return ssh.PublicKeysCallback(cb)
}

func urlUsernameOrLocal(url *url.URL) string {
	if url != nil && url.User != nil {
		if username := url.User.Username(); username != "" {
			return username
		}
	}

	var username string
	if u, err := user.Current(); err == nil {
		username = u.Username
	} else {
		username = os.Getenv("USER")
	}

	return username
}

func getHostWithPort(ep *url.URL) string {
	if addr, found := doGetHostWithPortFromSSHConfig(ep); found {
		return addr
	}

	addr := ep.Host
	port := ep.Port()
	if port == "" {
		addr = net.JoinHostPort(addr, DefaultSSHPort)
	}

	return addr
}

func doGetHostWithPortFromSSHConfig(ep *url.URL) (addr string, found bool) {
	if DefaultSSHConfig == nil {
		return
	}

	hostname := ep.Hostname()
	port := ep.Port()

	configHost := DefaultSSHConfig.Get(ep.Hostname(), "Hostname")
	if configHost != "" {
		hostname = configHost
		found = true
	}

	if !found {
		return
	}

	configPort := DefaultSSHConfig.Get(ep.Hostname(), "Port")
	if configPort != "" {
		if _, err := strconv.Atoi(configPort); err == nil {
			port = configPort
		}
	}

	addr = net.JoinHostPort(hostname, port)
	return
}

func setDefaultAuth(cc *ssh.ClientConfig, username string) {
	if cc.User == "" {
		cc.User = username
	}
	if len(cc.Auth) > 0 {
		return
	}
	a, _, err := sshagent.New()
	if err != nil {
		return
	}
	cc.Auth = append(cc.Auth, tracePublicKeysCallback(a.Signers))
}
