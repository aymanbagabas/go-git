// Package transportx provides a default Client constructor that wires
// all built-in transport implementations.
package transportx

import (
	transport "github.com/go-git/go-git/v6/x/transport"
	"github.com/go-git/go-git/v6/x/transport/file"
	xgit "github.com/go-git/go-git/v6/x/transport/git"
	xhttp "github.com/go-git/go-git/v6/x/transport/http"
	xssh "github.com/go-git/go-git/v6/x/transport/ssh"
)

// NewClient creates a Client with all built-in transports registered
// (file, git, http, https, ssh). Additional options are applied after
// built-in registration.
func NewClient(opts ...transport.ClientOption) *transport.Client {
	builtins := []transport.ClientOption{
		transport.WithScheme("file", file.NewFactory(nil)),
		transport.WithScheme("git", xgit.NewFactory()),
		transport.WithScheme("http", xhttp.NewFactory()),
		transport.WithScheme("https", xhttp.NewFactory()),
		transport.WithScheme("ssh", xssh.NewFactory()),
	}
	return transport.NewClient(append(builtins, opts...)...)
}
