package transfer

import "fmt"

type AuthMethod interface {
	fmt.Stringer
	Name() string
}
