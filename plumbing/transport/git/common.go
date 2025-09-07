// Package git implements the git transport protocol.
package git

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func init() {
	transport.Register("git", DefaultClient)
}

// DefaultClient is the default git client.
var DefaultClient = transport.NewPackTransport(&runner{})

const DefaultPort = 9418

type runner struct{}

func (r *runner) Run(ctx context.Context, cmd *transport.Cmd, ep *transport.Endpoint, auth transport.AuthMethod) error {
	// TODO: Use the ctx to set deadlines on the TCP connection.
	switch transport.GitService(cmd.Path) {
	case transport.UploadPackService, transport.ReceivePackService:
		// do nothing
	default:
		return transport.ErrUnsupportedService
	}

	if len(cmd.Args) < 2 {
		return fmt.Errorf("git: missing repository path")
	}

	s := &session{}
	s.command = cmd.Path
	s.endpoint = ep
	for _, env := range cmd.Env {
		if val, ok := strings.CutPrefix(env, "GIT_PROTOCOL="); ok {
			s.params = strings.Split(val, ":")
			break
		}
	}
	if err := s.connect(); err != nil {
		return err
	}

	cmd.Start = s.Start
	cmd.StderrPipe = s.StderrPipe
	cmd.StdinPipe = s.StdinPipe
	cmd.StdoutPipe = s.StdoutPipe
	cmd.Close = s.Close
	cmd.Sys = s

	return nil
}

type session struct {
	conn      net.Conn
	connected bool
	command   string
	endpoint  *transport.Endpoint
	params    []string
}

// Start executes the command sending the required message to the TCP connection
func (s *session) Start() error {
	req := packp.GitProtoRequest{
		RequestCommand: s.command,
		Pathname:       s.endpoint.Path,
		ExtraParams:    s.params,
	}

	host := s.endpoint.Host
	if s.endpoint.Port != DefaultPort {
		host = net.JoinHostPort(s.endpoint.Host, strconv.Itoa(s.endpoint.Port))
	}

	req.Host = host

	return req.Encode(s.conn)
}

func (s *session) connect() error {
	if s.connected {
		return transport.ErrAlreadyConnected
	}

	var err error
	s.conn, err = net.Dial("tcp", s.getHostWithPort())
	if err != nil {
		return err
	}

	s.connected = true
	return nil
}

func (s *session) getHostWithPort() string {
	host := s.endpoint.Host
	port := s.endpoint.Port
	if port <= 0 {
		port = DefaultPort
	}

	return net.JoinHostPort(host, strconv.Itoa(port))
}

// StderrPipe git protocol doesn't have any dedicated error channel
func (s *session) StderrPipe() (io.Reader, error) {
	return nil, nil
}

// StdinPipe returns the underlying connection as WriteCloser, wrapped to prevent
// call to the Close function from the connection, a command execution in git
// protocol can't be closed or killed
func (s *session) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(s.conn), nil
}

// StdoutPipe returns the underlying connection as Reader
func (s *session) StdoutPipe() (io.Reader, error) {
	return s.conn, nil
}

// Close closes the TCP connection and connection.
func (s *session) Close() error {
	if !s.connected {
		return nil
	}

	s.connected = false
	return s.conn.Close()
}
