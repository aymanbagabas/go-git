package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"

	sshinternal "github.com/go-git/go-git/v6/internal/ssh"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/kevinburke/ssh_config"
	sshagent "github.com/xanzy/ssh-agent"
	"golang.org/x/crypto/ssh"
)

// DefaultSSHPort is the default port for SSH connections.
const DefaultSSHPort = "22"

// DefaultSSHConfig is the reader used to access parameters stored in the
// system's ssh_config files. If nil all the ssh_config are ignored.
var DefaultSSHConfig = ssh_config.DefaultUserSettings

// DefaultSSHTransport is the default SSH transport.
var DefaultSSHTransport = &SSHClient{
	// ClientConfig: &ssh.ClientConfig{},
	// Dialer:       proxy.FromEnvironmentUsing(DefaultDialer),
}

// SSHClient is a mechanism for SSH Git transports.
type SSHClient struct {
	Client *ssh.Client
}

var _ Connectable = &SSHClient{}

//
// // ConfigureDialer configures a new copy of the transport with the given
// // dialer.
// // It implements the [DialerConfigurer] interface.
// func (t *SSHClient) ConfigureDialer(dialer proxy.Dialer) (TransportOld, error) {
// 	t2 := t.Clone()
// 	t2.Dialer = dialer
// 	return t2, nil
// }
//
// // ConfigureProxyURL configures a new copy of the transport with the given
// // proxy URL.
// // It implements the [ProxyURLConfigurer] interface.
// func (t *SSHClient) ConfigureProxyURL(proxyURL *url.URL) (TransportOld, error) {
// 	t2 := t.Clone()
// 	dialer, err := proxy.FromURL(proxyURL, DefaultDialer)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to configure proxy URL: %w", err)
// 	}
// 	t2.Dialer = dialer
// 	return t2, nil
// }
//
// // ConfigureAuthMethod configures a new copy of the transport with the given
// // authentication method.
// // It implements the [AuthMethodConfigurer] interface.
// func (t *SSHClient) ConfigureAuthMethod(auth AuthMethod) (TransportOld, error) {
// 	t2 := t.Clone()
// 	if err := auth.SetAuth(t2); err != nil {
// 		return nil, fmt.Errorf("failed to configure auth method: %w", err)
// 	}
// 	return t2, nil
// }
//
// // Clone returns a new copy of the transport.
// func (t *SSHClient) Clone() *SSHClient {
// 	var cc ssh.ClientConfig
// 	if t.ClientConfig != nil {
// 		cc = *t.ClientConfig
// 	}
//
// 	return &SSHClient{
// 		ClientConfig: &cc,
// 		Dialer:       t.Dialer,
// 	}
// }

// Connect connects to a remote Git repository over SSH.
func (c *SSHClient) Connect(ctx context.Context, remoteURL *url.URL) (Runner, error) {
	if c.Client == nil {
		panic("SSH transport requires an ssh.Client")
	}

	// cc, err := defaultSSHConfig(remoteURL)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create default SSH client config: %w", err)
	// }
	//
	// // Merge user provided ClientConfig over the default one.
	// sshinternal.OverrideConfig(t.ClientConfig, cc)
	//
	// addr := sshHostPort(remoteURL)
	// if cc.HostKeyCallback == nil {
	// 	db, err := sshinternal.NewKnownHostsDB()
	// 	if err != nil {
	// 		return nil, err
	// 	}
	//
	// 	cc.HostKeyCallback = db.HostKeyCallback()
	// 	cc.HostKeyAlgorithms = db.HostKeyAlgorithms(addr)
	// } else if len(cc.HostKeyAlgorithms) == 0 {
	// 	// Set the HostKeyAlgorithms based on HostKeyCallback.
	// 	// For background see https://github.com/go-git/go-git/issues/411 as well as
	// 	// https://github.com/golang/go/issues/29286 for root cause.
	// 	db, err := sshinternal.NewKnownHostsDB()
	// 	if err != nil {
	// 		return nil, err
	// 	}
	//
	// 	// Note that the knownhost database is used, as it provides additional functionality
	// 	// to handle ssh cert-authorities.
	// 	cc.HostKeyAlgorithms = db.HostKeyAlgorithms(addr)
	// }
	//
	// trace.SSH.Printf("ssh: host key algorithms %s", cc.HostKeyAlgorithms)
	//
	// var cancel context.CancelFunc
	// if cc.Timeout > 0 {
	// 	ctx, cancel = context.WithTimeout(ctx, cc.Timeout)
	// } else {
	// 	ctx, cancel = context.WithCancel(ctx)
	// }
	//
	// defer cancel()
	//
	// var conn net.Conn
	// if d, ok := t.Dialer.(proxy.ContextDialer); ok {
	// 	conn, err = d.DialContext(ctx, "tcp", addr)
	// } else {
	// 	// Context is ignored
	// 	conn, err = t.Dialer.Dial("tcp", addr)
	// }
	//
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to dial SSH connection: %w", err)
	// }
	//
	// sconn, chans, reqs, err := ssh.NewClientConn(conn, addr, cc)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create SSH client connection: %w", err)
	// }
	//
	// c := ssh.NewClient(sconn, chans, reqs)
	return &SSHRunner{c: c.Client}, nil
}

// SSHRunner is a Git transport session over SSH.
type SSHRunner struct {
	c      *ssh.Client
	s      *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

var _ Runner = &SSHRunner{}

// Close implements Session.
func (s *SSHRunner) Close() error {
	// XXX: If we did read the full packfile, then the session might be already
	// closed.
	_ = s.s.Close()
	err := s.c.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}

// Start implements Session.
func (s *SSHRunner) Start(ctx context.Context, cmd *Cmd) error {
	var err error
	s.s, err = s.c.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}

	if cmd.Proto > protocol.V0 {
		if err := s.s.Setenv("GIT_PROTOCOL", protocol.FormatVersion(cmd.Proto)); err != nil {
			_ = s.Close()
			return fmt.Errorf("failed to set GIT_PROTOCOL env: %w", err)
		}
	}
	if err := s.s.Start(buildSSHCommand(cmd)); err != nil {
		_ = s.s.Close()
		return fmt.Errorf("failed to start SSH session: %w", err)
	}

	return nil
}

// StdinPipe implements Session.
func (s *SSHRunner) StdinPipe() (io.WriteCloser, error) {
	return s.s.StdinPipe()
}

// StdoutPipe implements Session.
func (s *SSHRunner) StdoutPipe() (io.Reader, error) {
	return s.s.StdoutPipe()
}

// StderrPipe implements [StderrPiper].
func (s *SSHRunner) StderrPipe() (io.Reader, error) {
	return s.s.StderrPipe()
}

func sshHostPort(ep *url.URL) string {
	if addr, found := sshHostPortFromConfig(ep); found {
		return addr
	}

	return hostPort(ep, DefaultSSHPort)
}

func sshHostPortFromConfig(ep *url.URL) (addr string, found bool) {
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

func defaultSSHConfig(ep *url.URL) (*ssh.ClientConfig, error) {
	var username string
	if ep.User != nil {
		username = ep.User.Username()
	}

	if username == "" {
		u, err := sshinternal.OsUsername()
		if err != nil {
			return nil, err
		}
		username = u
	}

	a, _, err := sshagent.New()
	if err != nil {
		return nil, fmt.Errorf("error creating SSH agent: %q", err)
	}
	cc := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{sshinternal.TracePublicKeysCallback(a.Signers)},
	}
	return cc, nil
}
