package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

// FetchPack fetches a packfile from the remote connection into the given
// storage repository.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	sess PackSession,
	conn Connection,
	progress sideband.Progress,
) (err error) {
	var reader io.Reader = conn
	var demuxer *sideband.Demuxer
	switch {
	case sess.Supports(capability.Sideband):
		demuxer = sideband.NewDemuxer(sideband.Sideband, reader)
	case sess.Supports(capability.Sideband64k):
		demuxer = sideband.NewDemuxer(sideband.Sideband64k, reader)
	}

	if demuxer != nil {
		demuxer.Progress = progress
		reader = demuxer
	}

	return packfile.UpdateObjectStorage(st, reader)
}
