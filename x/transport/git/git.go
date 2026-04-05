// Package git implements the Git TCP transport for the new transport API.
package git

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	transport "github.com/go-git/go-git/v6/x/transport"
)

// DefaultPort is the default port for the git protocol.
const DefaultPort = 9418

// NewFactory returns a transport.Factory that creates Git TCP transports.
func NewFactory() transport.Factory {
	return func(opts transport.ClientOptions) transport.Transport {
		return &gitTransport{opts: opts}
	}
}

type gitTransport struct {
	opts transport.ClientOptions
}

func (t *gitTransport) Open(ctx context.Context, req *transport.Request) (transport.Session, error) {
	rwc, err := t.Connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewStreamSession(rwc), nil
}

func (t *gitTransport) Connect(ctx context.Context, req *transport.Request) (io.ReadWriteCloser, error) {
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}
	addr := net.JoinHostPort(host, port)

	dialFn := t.opts.Dial.DialContext
	if dialFn == nil {
		dialFn = (&net.Dialer{}).DialContext
	}

	if t.opts.Proxy.DialProxy != nil {
		dialFn = t.opts.Proxy.DialProxy(dialFn)
	}

	conn, err := dialFn(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	proto := packp.GitProtoRequest{
		RequestCommand: req.Command,
		Pathname:       req.URL.Path,
		Host:           net.JoinHostPort(host, port),
		ExtraParams:    transport.GitProtocolExtraParams(req.Protocol),
	}

	if err := proto.Encode(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("git: encode proto request: %w", err)
	}

	return conn, nil
}
