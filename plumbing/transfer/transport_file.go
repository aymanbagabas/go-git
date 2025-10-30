package transfer

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// FileTransport is the transport mechanism for local file system Git
// repositories.
type FileTransport struct {
	Loader Loader
}

// Connect connects to a local file system Git repository.
func (t *FileTransport) Connect(ctx context.Context, cmd *Cmd) (Conn, error) {
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

	c := newFileConn()

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
				io.NopCloser(c.stdin),
				c.stdout,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				// TODO: write pktline error directly
				_, _ = fmt.Fprintln(c.stderr, err)
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
				io.NopCloser(c.stdin),
				c.stdout,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				// TODO: write pktline error directly
				_, _ = fmt.Fprintln(c.stderr, err)
				_ = c.Close()
			}
		}()
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedService, cmd.Service)
	}

	return c, nil
}

// FileConn is a connection to a local file system Git repository.
type FileConn struct {
	stdin   *io.PipeReader
	stdinW  *io.PipeWriter
	stdout  *io.PipeWriter
	stdoutR *io.PipeReader
	stderr  *io.PipeWriter
	stderrR *io.PipeReader

	childIOFiles  []io.Closer
	parentIOFiles []io.Closer

	closed bool

	mu sync.Mutex
}

// IsStateless returns whether the connection is stateless.
func (c *FileConn) IsStateless() bool {
	return false
}

// Read reads data from the connection.
func (c *FileConn) Read(p []byte) (n int, err error) {
	return c.stdoutR.Read(p)
}

// Write writes data to the connection.
func (c *FileConn) Write(p []byte) (n int, err error) {
	return c.stdinW.Write(p)
}

func (c *FileConn) StdinPipe() (io.WriteCloser, error) {
	return c.stdinW, nil
}

func (c *FileConn) StdoutPipe() (io.Reader, error) {
	return c.stdoutR, nil
}

func (c *FileConn) StderrPipe() (io.Reader, error) {
	return c.stderrR, nil
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

func newFileConn() *FileConn {
	c := &FileConn{}
	stdinR, stdinW := io.Pipe()

	c.stdin = stdinR
	c.stdinW = stdinW

	stdoutR, stdoutW := io.Pipe()
	c.stdout = stdoutW
	c.stdoutR = stdoutR

	stderrR, stderrW := io.Pipe()
	c.stderr = stderrW
	c.stderrR = stderrR

	c.childIOFiles = append(c.childIOFiles, stdinR, stdoutW, stderrW)
	c.parentIOFiles = append(c.parentIOFiles, stdinW, stdoutR, stderrR)

	return c
}

func closeDiscriptors(fds []io.Closer) {
	for _, fd := range fds {
		fd.Close()
	}
}
