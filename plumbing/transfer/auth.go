package transfer

import "fmt"

// AuthMethod represents a way for authenticating with a remote Git server.
type AuthMethod interface {
	fmt.Stringer
	Name() string
	SetAuth(t Session) error
}
