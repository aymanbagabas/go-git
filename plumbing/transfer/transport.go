package transfer

import (
	"context"
	"fmt"
	"sync"
)

// Transport represents a transport mechanism that can be used to connect to a
// Git remote.
type Transport struct {
	mu  sync.Mutex
	reg map[string]Connector
}

// RegisterProtocol registers a new transport protocol.
func (t *Transport) RegisterProtocol(scheme string, connector Connector) {
	t.mu.Lock()
	if t.reg == nil {
		t.reg = make(map[string]Connector)
	}
	t.reg[scheme] = connector
	t.mu.Unlock()
}

// Connect connects to a remote Git repository using the appropriate protocol.
func (t *Transport) Connect(ctx context.Context, cmd *Cmd) (Conn, error) {
	t.mu.Lock()
	if t.reg == nil {
		t.reg = make(map[string]Connector)
	}
	connector, ok := t.reg[cmd.URL.Scheme]
	t.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unsupported protocol: %s", cmd.URL.Scheme)
	}

	return connector.Connect(ctx, cmd)
}
