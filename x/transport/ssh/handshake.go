package ssh

import (
	"context"

	transport "github.com/go-git/go-git/v6/x/transport"
)

// Handshake implements transport.PackTransport.
func (t *Transport) Handshake(ctx context.Context, req *transport.Request) (transport.PackSession, error) {
	sess, err := t.Open(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewStreamPackSession(sess, req.Command)
}

var _ transport.PackTransport = (*Transport)(nil)
