package transport

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

// FetchPackRequest contains the parameters for a fetch-pack request.
// This is used during the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
type FetchPackRequest struct {
	// Wants is the list of references to fetch.
	Wants []plumbing.Hash

	// Haves is the list of references the client already has.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool

	// Progress is the progress sideband.
	Progress sideband.Progress
}

// Negotiate returns the result of the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
func Negotiate(
	ctx context.Context,
	st storage.Storer,
	req *FetchPackRequest,
	s PackSession,
	conn Connection,
) (err error) {
	if len(req.Wants) == 0 {
		return fmt.Errorf("no wants specified")
	}

	// Create upload-request
	upreq := packp.NewUploadRequest()
	if s.Supports(capability.MultiACKDetailed) {
		upreq.Capabilities.Set(capability.MultiACKDetailed)
	} else if s.Supports(capability.MultiACK) {
		upreq.Capabilities.Set(capability.MultiACK)
	}

	if s.Supports(capability.Sideband64k) {
		upreq.Capabilities.Set(capability.Sideband64k)
	} else if s.Supports(capability.Sideband) {
		upreq.Capabilities.Set(capability.Sideband)
	}

	if s.Supports(capability.ThinPack) {
		upreq.Capabilities.Set(capability.ThinPack)
	}

	if s.Supports(capability.OFSDelta) {
		upreq.Capabilities.Set(capability.OFSDelta)
	}

	if s.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent())
	}

	upreq.Wants = req.Wants

	if req.Depth != 0 {
		upreq.Depth = packp.DepthCommits(req.Depth)
		upreq.Capabilities.Set(capability.Shallow)
		upreq.Shallows, err = st.Shallow()
		if err != nil {
			return err
		}
	}

	// XXX: empty request means haves are a subset of wants, in that case we have
	// everything we asked for. Close the connection and return nil.
	if isSubset(req.Haves, req.Wants) && len(upreq.Shallows) == 0 {
		return pktline.WriteFlush(conn)
	}

	if req.Progress == nil {
		upreq.Capabilities.Set(capability.NoProgress)
	}

	if req.IncludeTags {
		upreq.Capabilities.Set(capability.IncludeTag)
	}

	// Create upload-haves
	uphav := packp.UploadHaves{}
	uphav.Haves = req.Haves

	var (
		done  bool
		shupd packp.ShallowUpdate
		srvrs packp.ServerResponse
	)
	isMultiAck := s.Supports(capability.MultiACK) ||
		s.Supports(capability.MultiACKDetailed)

	for !done {
		if err := upreq.Encode(conn); err != nil {
			return fmt.Errorf("sending upload-request: %s", err)
		}

		// Decode shallow-update
		if req.Depth != 0 {
			if err := shupd.Decode(conn); err != nil {
				return fmt.Errorf("decoding shallow-update: %s", err)
			}

			// Update shallow
			defer func() {
				if err == nil {
					err = updateShallow(st, &shupd)
				}
			}()
		}

		// Encode upload-haves
		// TODO: support multi_ack and multi_ack_detailed caps
		if err := uphav.Encode(conn, true); err != nil {
			return fmt.Errorf("sending upload-haves: %s", err)
		}

		// Decode server-response
		if err := srvrs.Decode(conn, isMultiAck); err != nil {
			return fmt.Errorf("decoding server-response: %s", err)
		}

		// Let the server know we're done
		if _, err := pktline.WritePacketf(conn, "done\n"); err != nil {
			return fmt.Errorf("sending done: %s", err)
		}

		done = true

		// Decode final ACK/NAK
		_, finalp, err := pktline.ReadPacket(conn)
		if err != nil {
			return fmt.Errorf("reading final ACK/NAK: %s", err)
		}

		finalr := bytes.NewReader(finalp)
		if err := srvrs.Decode(finalr, isMultiAck); err != nil {
			return fmt.Errorf("decoding final ACK/NAK: %s", err)
		}
	}

	return nil
}

func isSubset(needle []plumbing.Hash, haystack []plumbing.Hash) bool {
	for _, h := range needle {
		found := false
		for _, oh := range haystack {
			if h == oh {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

func updateShallow(st storage.Storer, shupd *packp.ShallowUpdate) error {
	if len(shupd.Shallows) == 0 {
		return nil
	}

	shallows, err := st.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range shupd.Shallows {
		for _, oldS := range shallows {
			if s == oldS {
				continue outer
			}
		}
		shallows = append(shallows, s)
	}

	return st.SetShallow(shallows)
}
