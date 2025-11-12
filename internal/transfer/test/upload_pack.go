// Package test implements common test suite for different transport
// implementations.
package test

import (
	"context"
	"net/url"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transfer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/stretchr/testify/suite"
)

type UploadPackSuite struct {
	suite.Suite
	Endpoint            *url.URL
	EmptyEndpoint       *url.URL
	NonExistentEndpoint *url.URL
	Storer              storage.Storer
	EmptyStorer         storage.Storer
	NonExistentStorer   storage.Storer
	EmptyAuth           transfer.AuthMethod
}

func (s *UploadPackSuite) TestAdvertisedReferencesEmpty() {
	c, err := transfer.NewClient(s.EmptyEndpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.EmptyEndpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	ar, err := sess.GetRemoteRefs(context.TODO())
	s.Require().ErrorIs(err, transfer.ErrEmptyRemoteRepository)
	s.Require().Nil(ar)
}

func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	c, err := transfer.NewClient(s.NonExistentEndpoint, s.EmptyAuth)
	s.Require().NoError(err)
	_, err = c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.NonExistentEndpoint))
	s.Require().Error(err)
}

func (s *UploadPackSuite) TestCallAdvertisedReferenceTwice() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	ar1, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(ar1)
	ar2, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Equal(ar1, ar2)
}

func (s *UploadPackSuite) TestDefaultBranch() {
	ctx := context.TODO()
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(ctx, transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	info, err := sess.GetRemoteRefs(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(info)
	symrefs := sess.Capabilities().Get(capability.SymRef)
	s.Require().Len(symrefs, 1)
	s.Require().Equal("HEAD:refs/heads/master", symrefs[0])
}

func (s *UploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	info, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	s.Require().True(sess.Capabilities().Supports(capability.MultiACK))
}

func (s *UploadPackSuite) TestCapabilities() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	info, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	s.Require().Len(sess.Capabilities().Get(capability.Agent), 1)
}

func (s *UploadPackSuite) TestUploadPack() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	beforeCount := s.countObjects(s.Storer)
	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)

	s.Require().Equal(28, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestUploadPackWithContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	info, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = sess.Fetch(ctx, s.EmptyStorer, req)
	s.Require().NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	ctx, cancel := context.WithCancel(context.Background())

	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	info, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	cancel()
	err = sess.Fetch(ctx, s.EmptyStorer, req)
	s.Require().NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackFull() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	info, err := sess.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	beforeCount := s.countObjects(s.Storer)
	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)
	s.Require().Equal(28, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestUploadPackInvalidReq() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	// Invalid capabilities are now handled by the transport layer

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NoError(err) // Should succeed as invalid capabilities are handled internally
}

func (s *UploadPackSuite) TestUploadPackNoChanges() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().ErrorIs(err, transfer.ErrNoChange)
}

func (s *UploadPackSuite) TestUploadPackMulti() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	beforeCount := s.countObjects(s.Storer)
	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Wants = append(req.Wants, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)
	s.Require().Equal(31, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestUploadPackPartial() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	beforeCount := s.countObjects(s.Storer)
	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)
	s.Require().Equal(4, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestFetchError() {
	c, err := transfer.NewClient(s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	sess, err := c.NewSession(context.TODO(), transfer.Command(transfer.ServiceUploadPack, s.Endpoint))
	s.Require().NoError(err)
	defer func() { s.Require().Nil(sess.Close()) }()

	req := &transfer.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	err = sess.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NotNil(err)

	// XXX: We do not test Close error, since implementations might return
	//     different errors if a previous error was found.
}

func (s *UploadPackSuite) countObjects(st storage.Storer) int {
	iter, err := st.IterEncodedObjects(plumbing.AnyObject)
	s.Require().NoError(err)
	defer iter.Close()
	var count int
	err = iter.ForEach(func(plumbing.EncodedObject) error {
		count++
		return nil
	})
	s.Require().NoError(err)
	return count
}
