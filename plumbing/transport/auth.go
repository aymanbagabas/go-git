package transport

import "fmt"

// AuthMethod is an interface for an authentication method for a transport.
type AuthMethod interface {
	fmt.Stringer
	Name() string
}
