package transfer

import (
	"context"

	"golang.org/x/crypto/ssh"
)

// SSHTransport is a mechanism for SSH Git transports.
type SSHTransport struct {
	ClientConfig *ssh.ClientConfig
}

// Connect connects to a remote Git repository over SSH.
func (t *SSHTransport) Connect(ctx context.Context, cmd *Cmd) (Conn, error) {
	// Establish SSH connection
}

// SSHConn is a Git transport connection over SSH.
type SSHConn struct {
	c *ssh.Client
	s *ssh.Session
}
