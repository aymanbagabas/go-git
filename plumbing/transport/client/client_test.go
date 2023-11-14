package client

import (
	"net/http"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct{}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) TestNewClientSSH(c *C) {
	e, err := transport.NewEndpoint("ssh://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := NewClient(e)
	c.Assert(err, IsNil)
	c.Assert(output, NotNil)
}

func (s *ClientSuite) TestNewClientUnknown(c *C) {
	e, err := transport.NewEndpoint("unknown://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = NewClient(e)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestNewClientNil(c *C) {
	Protocols["newscheme"] = nil
	e, err := transport.NewEndpoint("newscheme://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = NewClient(e)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestInstallProtocol(c *C) {
	InstallProtocol("newscheme", &dummyClient{})
	c.Assert(Protocols["newscheme"], NotNil)
}

func (s *ClientSuite) TestInstallProtocolNilValue(c *C) {
	InstallProtocol("newscheme", &dummyClient{})
	InstallProtocol("newscheme", nil)

	_, ok := Protocols["newscheme"]
	c.Assert(ok, Equals, false)
}

type dummyClient struct {
	*http.Client
}

func (*dummyClient) NewSession(string, *transport.Endpoint, transport.AuthMethod) (
	transport.Session, error) {
	return nil, nil
}
