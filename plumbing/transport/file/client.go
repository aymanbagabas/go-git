// Package file implements the file transport protocol.
package file

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

func init() {
	transport.Register("file", DefaultTransport)
}

// DefaultTransport is the default local client.
var DefaultTransport = NewTransport(nil)

type runner struct {
	loader transport.Loader
}

// NewTransport returns a new file transport that users go-git built-in server
// implementation to serve repositories.
func NewTransport(loader transport.Loader) transport.Transport {
	if loader == nil {
		loader = transport.DefaultLoader
	}
	return transport.NewPackTransport(&runner{loader: loader})
}

func (r *runner) Run(ctx context.Context, cmd *transport.Cmd, ep *transport.Endpoint, auth transport.AuthMethod) error {
	switch transport.GitService(cmd.Path) {
	case transport.UploadPackService, transport.ReceivePackService:
		// do nothing
	default:
		return transport.ErrUnsupportedService
	}

	if len(cmd.Args) < 2 {
		return fmt.Errorf("file: missing repository path")
	}

	s := &session{ctx: ctx, loader: r.loader, errc: make(chan error, 1), svc: cmd.Path}
	s.ep = ep

	for _, env := range cmd.Env {
		if after, ok := strings.CutPrefix(env, "GIT_PROTOCOL="); ok {
			s.gitProtocol = after
			break
		}
	}

	cmd.Start = s.Start
	cmd.StderrPipe = s.StderrPipe
	cmd.StdinPipe = s.StdinPipe
	cmd.StdoutPipe = s.StdoutPipe
	cmd.Close = s.Close
	cmd.Sys = s

	return nil
}

type session struct {
	loader      transport.Loader
	ctx         context.Context
	ep          *transport.Endpoint
	svc         string
	gitProtocol string

	stdin  *io.PipeReader
	stdinW *io.PipeWriter
	stdout *io.PipeWriter
	stderr *io.PipeWriter

	childIOFiles  []io.Closer
	parentIOFiles []io.Closer

	closed bool
	errc   chan error
}

func (r *session) Start() error {
	st, err := r.loader.Load(r.ep)
	if err != nil {
		return err
	}

	switch transport.GitService(r.svc) {
	case transport.UploadPackService:
		opts := &transport.UploadPackOptions{
			GitProtocol: r.gitProtocol,
		}
		go func() {
			if err := transport.UploadPack(
				r.ctx,
				st,
				io.NopCloser(r.stdin),
				r.stdout,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				_, _ = fmt.Fprintln(r.stderr, err)
				_ = r.Close()
			}
		}()
		return nil
	case transport.ReceivePackService:
		opts := &transport.ReceivePackOptions{
			GitProtocol: r.gitProtocol,
		}
		go func() {
			if err := transport.ReceivePack(
				r.ctx,
				st,
				io.NopCloser(r.stdin),
				r.stdout,
				opts,
			); err != nil {
				_, _ = fmt.Fprintln(r.stderr, err)
				_ = r.Close()
			}
		}()
		return nil
	}
	return fmt.Errorf("unsupported service: %s", r.svc)
}

func (r *session) StderrPipe() (io.Reader, error) {
	pr, pw := io.Pipe()

	r.stderr = pw
	r.childIOFiles = append(r.childIOFiles, pw)
	r.parentIOFiles = append(r.parentIOFiles, pr)

	return pr, nil
}

func (r *session) StdinPipe() (io.WriteCloser, error) {
	pr, pw := io.Pipe()

	r.stdin = pr
	r.stdinW = pw
	r.childIOFiles = append(r.childIOFiles, pr)
	r.parentIOFiles = append(r.parentIOFiles, pw)

	return pw, nil
}

func (r *session) StdoutPipe() (io.Reader, error) {
	pr, pw := io.Pipe()

	r.stdout = pw
	r.childIOFiles = append(r.childIOFiles, pw)
	r.parentIOFiles = append(r.parentIOFiles, pr)

	return pr, nil
}

// Close waits for the command to exit.
func (r *session) Close() (err error) {
	if r.closed {
		return nil
	}

	closeDiscriptors(r.childIOFiles)
	closeDiscriptors(r.parentIOFiles)
	r.closed = true

	return
}

func closeDiscriptors(fds []io.Closer) {
	for _, fd := range fds {
		fd.Close()
	}
}
