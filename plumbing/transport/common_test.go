package transport

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

func TestCommonSuite(t *testing.T) {
	suite.Run(t, new(CommonSuite))
}

type CommonSuite struct {
	suite.Suite
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError() {
	stderr := "something"

	ep, err := NewEndpoint("https://example.com/foo.git")
	s.NoError(err)
	client := NewPackTransport(mockRunner{stderr: bytes.NewBufferString(stderr)})
	sess, err := client.NewSession(nil, ep, nil)
	if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), UploadPackService)
	s.Error(err)
}
