package transfer

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// FetchRequest contains the parameters for a fetch-pack request.
// This is used during the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
type FetchRequest struct {
	// Progress is the progress sideband.
	Progress sideband.Progress

	// Wants is the list of references to fetch.
	// TODO: Build this slice in the transport package.
	Wants []plumbing.Hash

	// Haves is the list of references the client already has.
	// TODO: Build this slice in the transport package.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// Filter holds the filters to be applied when deciding what
	// objects will be added to the packfile.
	Filter packp.Filter

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool
}

// FetchPack fetches a packfile from the remote connection into the given
// storage repository and updates the shallow information.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	conn Session,
	packf io.ReadCloser,
	shallowInfo *packp.ShallowUpdate,
	req *FetchRequest,
) (err error) {
	packf = ioutil.NewContextReadCloser(ctx, packf)

	// Do we have sideband enabled?
	var demuxer *sideband.Demuxer
	var reader io.Reader = packf
	caps := conn.Capabilities()
	if caps.Supports(capability.Sideband64k) {
		demuxer = sideband.NewDemuxer(sideband.Sideband64k, reader)
	} else if caps.Supports(capability.Sideband) {
		demuxer = sideband.NewDemuxer(sideband.Sideband, reader)
	}

	if demuxer != nil && req.Progress != nil {
		demuxer.Progress = req.Progress
		reader = demuxer
	}

	if err := packfile.UpdateObjectStorage(st, reader); err != nil {
		return fmt.Errorf("updating object storage: %w", err)
	}

	if err := packf.Close(); err != nil {
		return fmt.Errorf("closing packfile reader: %w", err)
	}

	// Update shallow
	if shallowInfo != nil {
		if err := updateShallow(st, shallowInfo); err != nil {
			return fmt.Errorf("updating shallow info: %w", err)
		}
	}

	return nil
}

func updateShallow(st storage.Storer, shallowInfo *packp.ShallowUpdate) error {
	shallows, err := st.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range shallowInfo.Shallows {
		for _, oldS := range shallows {
			if s == oldS {
				continue outer
			}
		}
		shallows = append(shallows, s)
	}

	// unshallow commits
	for _, s := range shallowInfo.Unshallows {
		for i, oldS := range shallows {
			if s == oldS {
				shallows = append(shallows[:i], shallows[i+1:]...)
				break
			}
		}
	}

	return st.SetShallow(shallows)
}
