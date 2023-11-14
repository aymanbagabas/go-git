package transport

import (
	"net/http"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	_ "github.com/go-git/go-git/v5/plumbing/transport/file"
	_ "github.com/go-git/go-git/v5/plumbing/transport/git"
	_ "github.com/go-git/go-git/v5/plumbing/transport/http"
	_ "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct{}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) TestNewClientSSH(c *C) {
	e, err := transport.NewEndpoint("ssh://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := transport.NewClient(e)
	c.Assert(err, IsNil)
	c.Assert(output, NotNil)
}

func (s *ClientSuite) TestNewClientUnknown(c *C) {
	e, err := transport.NewEndpoint("unknown://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = transport.NewClient(e)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestNewClientNil(c *C) {
	transport.Register("newscheme", nil)
	e, err := transport.NewEndpoint("newscheme://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = transport.NewClient(e)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestInstallProtocol(c *C) {
	transport.Register("newscheme", &dummyClient{})
	t, ok := transport.Get("newscheme")
	c.Assert(t, NotNil)
	c.Assert(ok, Equals, true)
}

func (s *ClientSuite) TestInstallProtocolNilValue(c *C) {
	transport.Register("newscheme", &dummyClient{})
	transport.Unregister("newscheme")

	t, ok := transport.Get("newscheme")
	c.Assert(t, IsNil)
	c.Assert(ok, Equals, false)
}

type dummyClient struct {
	*http.Client
}

func (*dummyClient) NewSession(string, *transport.Endpoint, transport.AuthMethod) (
	transport.Session, error) {
	return nil, nil
}
