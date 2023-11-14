package http

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

func (s *session) DiscoverReferences(ctx context.Context, forPush bool, _ *transport.SessionOptions) (*packp.AdvRefs, error) {
	return advertisedReferences(ctx, s, forPush)
}

func (s *session) Push(ctx context.Context, req *packp.ReferenceUpdateRequest) (
	*packp.ReportStatus, error) {
	url := fmt.Sprintf(
		"%s/%s",
		s.endpoint.String(), transport.ReceivePackServiceName,
	)

	buf := bytes.NewBuffer(nil)
	if err := req.Encode(buf); err != nil {
		return nil, err
	}

	res, err := s.doRequest(ctx, http.MethodPost, url, buf)
	if err != nil {
		return nil, err
	}

	r, err := ioutil.NonEmptyReader(res.Body)
	if err == ioutil.ErrEmptyReader {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var d *sideband.Demuxer
	if req.Capabilities.Supports(capability.Sideband64k) {
		d = sideband.NewDemuxer(sideband.Sideband64k, r)
	} else if req.Capabilities.Supports(capability.Sideband) {
		d = sideband.NewDemuxer(sideband.Sideband, r)
	}
	if d != nil {
		d.Progress = req.Progress
		r = d
	}

	rc := ioutil.NewReadCloser(r, res.Body)

	report := packp.NewReportStatus()
	if err := report.Decode(rc); err != nil {
		return nil, err
	}

	return report, report.Error()
}
