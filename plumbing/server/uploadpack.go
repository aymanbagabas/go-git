package server

import (
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

func UploadPack(ctx context.Context, st storer.Storer, req *packp.UploadPackRequest) (*packp.UploadPackResponse, error) {
	objs, err := objectsToUpload(st, req)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	e := packfile.NewEncoder(pw, st, false)
	go func() {
		// TODO: plumb through a pack window.
		_, err := e.Encode(objs, 10)
		pw.CloseWithError(err)
	}()

	return packp.NewUploadPackResponseWithPackfile(req,
		ioutil.NewContextReadCloser(ctx, pr),
	), nil
}

func objectsToUpload(st storer.Storer, req *packp.UploadPackRequest) ([]plumbing.Hash, error) {
	haves, err := revlist.Objects(st, req.Haves, nil)
	if err != nil {
		return nil, err
	}

	return revlist.Objects(st, req.Wants, haves)
}
