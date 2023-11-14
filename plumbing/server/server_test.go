package server_test

// import (
// 	"testing"
//
// 	"github.com/go-git/go-git/v5/plumbing/cache"
// 	"github.com/go-git/go-git/v5/plumbing/server"
// 	"github.com/go-git/go-git/v5/plumbing/transport"
// 	"github.com/go-git/go-git/v5/plumbing/transport/test"
// 	"github.com/go-git/go-git/v5/storage/filesystem"
// 	"github.com/go-git/go-git/v5/storage/memory"
//
// 	fixtures "github.com/go-git/go-git-fixtures/v4"
// 	. "gopkg.in/check.v1"
// )
//
// func Test(t *testing.T) { TestingT(t) }
//
// type BaseSuite struct {
// 	fixtures.Suite
// 	test.ReceivePackSuite
//
// 	loader server.MapLoader
// }
//
// func (s *BaseSuite) SetUpSuite(c *C) {
// 	s.loader = server.MapLoader{}
// }
//
// func (s *BaseSuite) TearDownSuite(c *C) {
// 	c.Skip("fixme")
// }
//
// func (s *BaseSuite) prepareRepositories(c *C) {
// 	var err error
//
// 	fs := fixtures.Basic().One().DotGit()
// 	s.Endpoint, err = transport.NewEndpoint(fs.Root())
// 	c.Assert(err, IsNil)
// 	s.loader[s.Endpoint.String()] = filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
//
// 	s.EmptyEndpoint, err = transport.NewEndpoint("/empty.git")
// 	c.Assert(err, IsNil)
// 	s.loader[s.EmptyEndpoint.String()] = memory.NewStorage()
//
// 	s.NonExistentEndpoint, err = transport.NewEndpoint("/non-existent.git")
// 	c.Assert(err, IsNil)
// }
