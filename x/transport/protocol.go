package transport

import (
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// GitProtocolEnv returns the value for the GIT_PROTOCOL environment variable
// corresponding to the given protocol version. Returns an empty string for
// protocol V0, which does not use GIT_PROTOCOL.
func GitProtocolEnv(v protocol.Version) string {
	switch v {
	case protocol.V0, protocol.Undefined:
		return ""
	default:
		return fmt.Sprintf("version=%s", v)
	}
}

// GitProtocolExtraParams returns the extra parameters for the git:// protocol
// request corresponding to the given protocol version. Returns nil for
// protocol V0.
func GitProtocolExtraParams(v protocol.Version) []string {
	if s := GitProtocolEnv(v); s != "" {
		return []string{s}
	}
	return nil
}
