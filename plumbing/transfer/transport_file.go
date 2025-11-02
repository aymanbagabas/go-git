package transfer

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// DefaultFileTransport is the default file transport.
var DefaultFileTransport = &FileTransport{
	Loader: DefaultLoader,
}

// FileTransport is the transport mechanism for local file system Git
// repositories.
type FileTransport struct {
	Loader Loader
}

var _ Transport = &FileTransport{}

// Clone returns a new copy of the transport.
func (t *FileTransport) Clone() *FileTransport {
	return &FileTransport{
		Loader: t.Loader,
	}
}

// Connect connects to a local file system Git repository.
func (t *FileTransport) Connect(ctx context.Context, cmd *Cmd) (net.Conn, error) {
	if t.Loader == nil {
		panic("file transport requires a Loader")
	}
	st, err := t.Loader.Load(cmd.URL)
	if err != nil {
		return nil, err
	}

	var gitProto string
	if cmd.Proto > protocol.V0 {
		gitProto = protocol.FormatVersion(cmd.Proto)
	}

	c, err := newFileConn(cmd.URL.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file connection: %w", err)
	}

	switch cmd.Service {
	case ServiceUploadPack:
		opts := &UploadPackOptions{
			GitProtocol:  gitProto,
			StatelessRPC: false,
		}
		go func() {
			if err := UploadPack(
				ctx,
				st,
				io.NopCloser(c.input),
				c.output,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				_, _ = pktline.WriteError(c.output, err)
				_ = c.Close()
			}
		}()
	case ServiceReceivePack:
		opts := &ReceivePackOptions{
			GitProtocol:  gitProto,
			StatelessRPC: false,
		}
		go func() {
			if err := ReceivePack(
				ctx,
				st,
				io.NopCloser(c.input),
				c.output,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				_, _ = pktline.WriteError(c.output, err)
				_ = c.Close()
			}
		}()
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedService, cmd.Service)
	}

	return c, nil
}

// NewSession implements Transport.
func (t *FileTransport) NewSession(ctx context.Context, cmd *Cmd) (Session, error) {
	c, err := t.Connect(ctx, cmd)
	if err != nil {
		return nil, err
	}

	return NewPackSession(ctx, c, cmd)
}

// FileConn is a connection to a local file system Git repository.
type FileConn struct {
	input   *os.File
	inputW  *os.File
	output  *os.File
	outputR *os.File

	childIOFiles  []io.Closer
	parentIOFiles []io.Closer

	repoPath string
	closed   bool

	mu sync.Mutex
}

// Read reads data from the connection.
func (c *FileConn) Read(p []byte) (n int, err error) {
	return c.outputR.Read(p)
}

// Write writes data to the connection.
func (c *FileConn) Write(p []byte) (n int, err error) {
	return c.inputW.Write(p)
}

// Close waits for the command to exit.
func (c *FileConn) Close() (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	closeDiscriptors(c.childIOFiles)
	closeDiscriptors(c.parentIOFiles)
	c.closed = true

	return
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *FileConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *FileConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.outputR.SetDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *FileConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.inputW.SetDeadline(t)
}

type fileAddr string

func (a fileAddr) Network() string {
	return "file"
}

func (a fileAddr) String() string {
	return string(a)
}

// LocalAddr returns the local network address.
func (c *FileConn) LocalAddr() net.Addr {
	return fileAddr(c.repoPath)
}

// RemoteAddr returns the remote network address.
func (c *FileConn) RemoteAddr() net.Addr {
	return fileAddr(c.repoPath)
}

func newFileConn(repoPath string) (*FileConn, error) {
	c := &FileConn{
		repoPath: repoPath,
	}
	inputR, inputW, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	c.input = inputR
	c.inputW = inputW

	outputR, outputW, err := os.Pipe()
	if err != nil {
		_ = inputR.Close()
		_ = inputW.Close()
		return nil, err
	}

	c.output = outputW
	c.outputR = outputR

	c.childIOFiles = append(c.childIOFiles, inputR, outputW)
	c.parentIOFiles = append(c.parentIOFiles, inputW, outputR)

	return c, nil
}

func closeDiscriptors(fds []io.Closer) {
	for _, fd := range fds {
		fd.Close()
	}
}
