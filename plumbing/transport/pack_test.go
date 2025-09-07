package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/stretchr/testify/suite"
)

func TestCmdStartEOFSuite(t *testing.T) {
	suite.Run(t, new(CmdStartEOFSuite))
}

type CmdStartEOFSuite struct {
	suite.Suite
}

type mockStartEOFCommand struct {
	stdin         bytes.Buffer
	stdout        bytes.Buffer
	stderr        bytes.Buffer
	sessionOpened bool
}

func (c *mockStartEOFCommand) StderrPipe() (io.Reader, error) {
	return &c.stderr, nil
}

func (c *mockStartEOFCommand) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(&c.stdin), nil
}

func (c *mockStartEOFCommand) StdoutPipe() (io.Reader, error) {
	return &c.stdout, nil
}

func (c *mockStartEOFCommand) Start() error {
	c.sessionOpened = true
	return io.EOF
}

func (c *mockStartEOFCommand) Close() error {
	c.sessionOpened = false
	return nil
}

type mockStartEOFRunner struct {
	mockCmd *mockStartEOFCommand
}

func (c *mockStartEOFRunner) Run(_ context.Context, cmd *Cmd, _ *Endpoint, _ AuthMethod) error {
	c.mockCmd = &mockStartEOFCommand{}
	cmd.Start = c.mockCmd.Start
	cmd.StderrPipe = c.mockCmd.StderrPipe
	cmd.StdinPipe = c.mockCmd.StdinPipe
	cmd.StdoutPipe = c.mockCmd.StdoutPipe
	cmd.Close = c.mockCmd.Close
	return nil
}

func (s *CmdStartEOFSuite) TestCmdStartEOFConnectionLeakError() {
	client := NewPackTransport(&mockStartEOFRunner{})
	ep, err := NewEndpoint("file://foo")
	s.NoError(err)
	sess, err := client.NewSession(nil, ep, nil)
	if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), UploadPackService)
	s.ErrorIs(err, io.EOF)
	runrInterface := sess.(*PackSession).runr
	runr := runrInterface.(*mockStartEOFRunner)
	s.False(runr.mockCmd.sessionOpened)
}
