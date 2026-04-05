// Package file implements the file transport for the new transport API.
package file

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/storage"
	transport "github.com/go-git/go-git/v6/x/transport"
)

// ServerFunc is a function that runs a git server-side command over pipes.
// It reads from r, writes to w, and uses st for object storage.
type ServerFunc func(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, gitProtocol string) error

// defaultUploadPack wraps the old transport.UploadPack into a ServerFunc.
func defaultUploadPack(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, gitProtocol string) error {
	return transport.UploadPack(ctx, st, r, w, &transport.UploadPackOptions{
		GitProtocol:          gitProtocol,
		SkipDeltaCompression: true,
	})
}

// defaultReceivePack wraps the old transport.ReceivePack into a ServerFunc.
func defaultReceivePack(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, gitProtocol string) error {
	return transport.ReceivePack(ctx, st, r, w, &transport.ReceivePackOptions{
		GitProtocol: gitProtocol,
	})
}

// NewFactory returns a transport.Factory that creates file transports
// using the given Loader. If loader is nil, DefaultLoader is used.
func NewFactory(loader transport.Loader) transport.Factory {
	return func(_ transport.ClientOptions) transport.Transport {
		if loader == nil {
			loader = transport.DefaultLoader
		}
		return &fileTransport{
			loader:      loader,
			uploadPack:  defaultUploadPack,
			receivePack: defaultReceivePack,
		}
	}
}

type fileTransport struct {
	loader      transport.Loader
	uploadPack  ServerFunc
	receivePack ServerFunc
}

func (t *fileTransport) Open(ctx context.Context, req *transport.Request) (transport.Session, error) {
	rwc, err := t.Connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewStreamSession(rwc), nil
}

func (t *fileTransport) Connect(ctx context.Context, req *transport.Request) (io.ReadWriteCloser, error) {
	var serverFn ServerFunc
	switch req.Command {
	case "git-upload-pack":
		serverFn = t.uploadPack
	case "git-receive-pack":
		serverFn = t.receivePack
	default:
		return nil, fmt.Errorf("%w: %s", transport.ErrCommandUnsupported, req.Command)
	}

	st, err := t.loader.Load(req.URL)
	if err != nil {
		return nil, err
	}

	gitProtocol := transport.GitProtocolEnv(req.Protocol)

	pr, pw := io.Pipe()
	sr, sw := io.Pipe()

	rwc := &streamConn{
		Reader:    sr,
		Writer:    pw,
		closeFunc: func() error { _ = pw.Close(); return sr.Close() },
	}

	go func() {
		err := serverFn(ctx, st, io.NopCloser(pr), sw, gitProtocol)
		_ = sw.CloseWithError(err)
		_ = pr.Close()
	}()

	return rwc, nil
}

type streamConn struct {
	io.Reader
	io.Writer
	closeFunc func() error
}

func (c *streamConn) Close() error {
	return c.closeFunc()
}
