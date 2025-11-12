package transfer

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage"
)

// DefaultFileTransport is the default file transport.
var DefaultFileTransport = &FileClient{
	Loader: DefaultLoader,
}

// FileClient is a client that can connect to a local file system Git
// repository.
type FileClient struct {
	Loader Loader
}

var _ Connectable = &FileClient{}

// Clone returns a new copy of the transport.
func (t *FileClient) Clone() *FileClient {
	return &FileClient{
		Loader: t.Loader,
	}
}

// Connect connects to a local file system Git repository.
func (t *FileClient) Connect(ctx context.Context, remoteURL *url.URL) (Runner, error) {
	if t.Loader == nil {
		panic("file transport requires a Loader")
	}
	st, err := t.Loader.Load(remoteURL)
	if err != nil {
		return nil, err
	}

	return &FileSession{
		st: st,
	}, nil
}

// FileSession is a Git session over a local file system repository.
type FileSession struct {
	st storage.Storer

	input   *io.PipeReader
	inputW  *io.PipeWriter
	output  *io.PipeWriter
	outputR *io.PipeReader

	childIOFiles  []io.Closer
	parentIOFiles []io.Closer
}

var _ Runner = &FileSession{}

// Close implements Session.
func (s *FileSession) Close() error {
	closeDiscriptors(s.childIOFiles)
	closeDiscriptors(s.parentIOFiles)
	return nil
}

// Start implements Session.
func (s *FileSession) Start(ctx context.Context, cmd *Cmd) error {
	var gitProto string
	if cmd.Proto > protocol.V0 {
		gitProto = protocol.FormatVersion(cmd.Proto)
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
				s.st,
				io.NopCloser(s.input),
				s.output,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				_, _ = pktline.WriteError(s.output, err)
				_ = s.Close()
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
				s.st,
				io.NopCloser(s.input),
				s.output,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				_, _ = pktline.WriteError(s.output, err)
				_ = s.Close()
			}
		}()
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedService, cmd.Service)
	}

	return nil
}

// StdinPipe implements Session.
func (s *FileSession) StdinPipe() (io.WriteCloser, error) {
	inputR, inputW := io.Pipe()
	s.input = inputR
	s.inputW = inputW
	s.childIOFiles = append(s.childIOFiles, inputR)
	s.parentIOFiles = append(s.parentIOFiles, inputW)
	return inputW, nil
}

// StdoutPipe implements Session.
func (s *FileSession) StdoutPipe() (io.Reader, error) {
	outputR, outputW := io.Pipe()
	s.output = outputW
	s.outputR = outputR
	s.childIOFiles = append(s.childIOFiles, outputW)
	s.parentIOFiles = append(s.parentIOFiles, outputR)
	return outputR, nil
}

func closeDiscriptors(fds []io.Closer) {
	for _, fd := range fds {
		_ = fd.Close()
	}
}
