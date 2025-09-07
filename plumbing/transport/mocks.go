package transport

import (
	"bytes"
	"context"
	"io"

	"github.com/go-git/go-git/v6/utils/ioutil"
)

type mockRunner struct {
	stdin  *bytes.Buffer
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func (r mockRunner) StderrPipe() (io.Reader, error) {
	return r.stderr, nil
}

func (r mockRunner) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(r.stdin), nil
}

func (r mockRunner) StdoutPipe() (io.Reader, error) {
	return r.stdout, nil
}

func (r mockRunner) Start() error {
	return nil
}

func (r mockRunner) Close() error {
	return nil
}

func (r mockRunner) Run(_ context.Context, cmd *Cmd, _ *Endpoint, _ AuthMethod) error {
	if r.stdin == nil {
		r.stdin = &bytes.Buffer{}
	}
	if r.stdout == nil {
		r.stdout = &bytes.Buffer{}
	}
	if r.stderr == nil {
		r.stderr = &bytes.Buffer{}
	}
	cmd.Start = r.Start
	cmd.StderrPipe = r.StderrPipe
	cmd.StdinPipe = r.StdinPipe
	cmd.StdoutPipe = r.StdoutPipe
	cmd.Close = r.Close
	return nil
}
