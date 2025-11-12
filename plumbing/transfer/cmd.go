package transfer

import (
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

// DefaultProto is the default Git protocol version used by Go-Git client
// transfer.
const DefaultProto = protocol.V0

// Cmd is a Git command to be executed remotely.
type Cmd struct {
	// Service the Git service to be executed (e.g. git-upload-pack).
	Service string
	// Path is the repository path for the command.
	Path string
	// Proto the protocol version we're requesting (e.g. v0, v1, v2).
	Proto protocol.Version
}

// Command returns a new [Cmd] instance for the given service and repository
// URL.
func Command(svc, path string) *Cmd {
	return &Cmd{
		Service: svc,
		Path:    path,
		Proto:   DefaultProto,
	}
}

// buildSSHCommand builds the SSH command suitable for an SSH session.
func buildSSHCommand(cmd *Cmd) string {
	s := fmt.Sprintf("%s '%s'", cmd.Service, cmd.Path)
	return s
}

// buildGitCommand builds the Git command suitable for a Git TCP connection.
func buildGitCommand(cmd *Cmd, host string) packp.GitProtoRequest {
	var params string
	if cmd.Proto > 0 {
		params = protocol.FormatVersion(cmd.Proto)
	}
	return packp.GitProtoRequest{
		RequestCommand: cmd.Service,
		Pathname:       cmd.Path,
		ExtraParams:    []string{params},
		Host:           host,
	}
}
