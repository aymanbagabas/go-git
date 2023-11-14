package file

import (
	"context"
	"os"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/transport"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	CommonSuite
	test.ReceivePackSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpSuite(c *C) {
	s.CommonSuite.SetUpSuite(c)
	s.ReceivePackSuite.Client = DefaultTransport
}

func (s *ReceivePackSuite) SetUpTest(c *C) {
	fixture := fixtures.Basic().One()
	path := fixture.DotGit().Root()
	s.Endpoint = prepareRepo(c, path)

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	s.EmptyEndpoint = prepareRepo(c, path)

	s.NonExistentEndpoint = prepareRepo(c, "/non-existent")
}

func (s *ReceivePackSuite) TearDownTest(c *C) {
	s.Suite.TearDownSuite(c)
}

// TODO: fix test
func (s *ReceivePackSuite) TestCommandNoOutput(c *C) {
	c.Skip("failing test")

	if _, err := os.Stat("/bin/true"); os.IsNotExist(err) {
		c.Skip("/bin/true not found")
	}

	client := NewTransport("true", "true")
	session, err := client.NewSession(transport.ReceivePackServiceName, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.DiscoverReferences(context.TODO(), true, nil)
	c.Assert(err, IsNil)
	c.Assert(ar, IsNil)
}

func (s *ReceivePackSuite) TestMalformedInputNoErrors(c *C) {
	if _, err := os.Stat("/usr/bin/yes"); os.IsNotExist(err) {
		c.Skip("/usr/bin/yes not found")
	}

	client := NewTransport("yes", "yes")
	session, err := client.NewSession(transport.ReceivePackServiceName, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	ar, err := session.DiscoverReferences(context.TODO(), true, nil)
	c.Assert(err, NotNil)
	c.Assert(ar, IsNil)
}

func (s *ReceivePackSuite) TestNonExistentCommand(c *C) {
	cmd := "/non-existent-git"
	client := NewTransport(cmd, cmd)
	session, err := client.NewSession(transport.ReceivePackServiceName, s.Endpoint, s.EmptyAuth)
	c.Assert(err, ErrorMatches, ".*(no such file or directory.*|.*file does not exist)*.")
	c.Assert(session, IsNil)
}
