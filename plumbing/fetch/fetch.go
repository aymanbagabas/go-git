package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/internal/repository"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

var (
	ErrExactSHA1NotSupported = errors.New("server does not support exact SHA1 refspec")
	ErrForceNeeded           = errors.New("some refs were not updated")
)

type NoMatchingRefSpecError struct {
	refSpec config.RefSpec
}

func (e NoMatchingRefSpecError) Error() string {
	return fmt.Sprintf("couldn't find remote ref %q", e.refSpec.Src())
}

func (e NoMatchingRefSpecError) Is(target error) bool {
	_, ok := target.(NoMatchingRefSpecError)
	return ok
}

// FetchOptions describes how a fetch should be performed
type FetchOptions struct {
	RefSpecs []config.RefSpec
	// Depth limit fetching to the specified number of commits from the tip of
	// each remote branch history.
	Depth int
	// Progress is where the human readable information sent by the server is
	// stored, if nil nothing is stored and the capability (if supported)
	// no-progress, is sent to the server to avoid send this information.
	Progress sideband.Progress
	// Tags describe how the tags will be fetched from the remote repository,
	// by default is TagFollowing.
	Tags plumbing.TagMode
	// Force allows the fetch to update a local branch even when the remote
	// branch does not descend from it.
	Force bool
}

func newUploadPackRequest(o *FetchOptions, ar *packp.AdvRefs) (*packp.UploadPackRequest, error) {
	req := packp.NewUploadPackRequestFromCapabilities(ar.Capabilities)

	if o.Depth != 0 {
		req.Depth = packp.DepthCommits(o.Depth)
		if err := req.Capabilities.Set(capability.Shallow); err != nil {
			return nil, err
		}
	}

	if o.Progress == nil && ar.Capabilities.Supports(capability.NoProgress) {
		if err := req.Capabilities.Set(capability.NoProgress); err != nil {
			return nil, err
		}
	}

	isWildcard := true
	for _, s := range o.RefSpecs {
		if !s.IsWildcard() {
			isWildcard = false
			break
		}
	}

	if isWildcard && o.Tags == plumbing.TagFollowing && ar.Capabilities.Supports(capability.IncludeTag) {
		if err := req.Capabilities.Set(capability.IncludeTag); err != nil {
			return nil, err
		}
	}

	return req, nil
}

func isSupportedRefSpec(refs []config.RefSpec, ar *packp.AdvRefs) error {
	var containsIsExact bool
	for _, ref := range refs {
		if ref.IsExactSHA1() {
			containsIsExact = true
		}
	}

	if !containsIsExact {
		return nil
	}

	if ar.Capabilities.Supports(capability.AllowReachableSHA1InWant) ||
		ar.Capabilities.Supports(capability.AllowTipSHA1InWant) {
		return nil
	}

	return ErrExactSHA1NotSupported
}

func Fetch(ctx context.Context, st storage.Storer, s transport.UploadPackSession, o *FetchOptions) (sto storer.ReferenceStorer, err error) {
	ar, err := s.AdvertisedReferencesContext(ctx)
	if err != nil {
		return nil, err
	}

	req, err := newUploadPackRequest(o, ar)
	if err != nil {
		return nil, err
	}

	if err := isSupportedRefSpec(o.RefSpecs, ar); err != nil {
		return nil, err
	}

	remoteRefs, err := ar.AllReferences()
	if err != nil {
		return nil, err
	}

	localRefs, err := repository.References(st)
	if err != nil {
		return nil, err
	}

	refs, specToRefs, err := calculateRefs(o.RefSpecs, remoteRefs, o.Tags)
	if err != nil {
		return nil, err
	}

	if !req.Depth.IsZero() {
		req.Shallows, err = st.Shallow()
		if err != nil {
			return nil, fmt.Errorf("existing checkout is not shallow")
		}
	}

	req.Wants, err = getWants(st, refs, o.Depth)
	if err != nil {
		return nil, err
	}

	if len(req.Wants) > 0 {
		req.Haves, err = repository.GetHaves(localRefs, remoteRefs, st, o.Depth)
		if err != nil {
			return nil, err
		}

		if err = FetchPack(ctx, st, o, s, req); err != nil {
			return nil, err
		}
	}

	updated, err := updateLocalReferenceStorage(st, o.RefSpecs, refs, remoteRefs, specToRefs, o.Tags, o.Force)
	if err != nil {
		return nil, err
	}

	if !updated {
		updated, err = depthChanged(req.Shallows, st)
		if err != nil {
			return nil, fmt.Errorf("error checking depth change: %v", err)
		}
	}

	if !updated {
		return remoteRefs, plumbing.NoErrAlreadyUpToDate
	}

	return remoteRefs, nil
}

func FetchPack(ctx context.Context, st storage.Storer, o *FetchOptions, s transport.UploadPackSession,
	req *packp.UploadPackRequest) (err error) {

	reader, err := s.UploadPack(ctx, req)
	if err != nil {
		if errors.Is(err, transport.ErrEmptyUploadPackRequest) {
			// XXX: no packfile provided, everything is up-to-date.
			return nil
		}
		return err
	}

	defer ioutil.CheckClose(reader, &err)

	if err = repository.UpdateShallow(st, o.Depth, reader.Shallows); err != nil {
		return err
	}

	if err = packfile.UpdateObjectStorage(st,
		buildSidebandIfSupported(req.Capabilities, reader, o.Progress),
	); err != nil {
		return err
	}

	return err
}

func buildSidebandIfSupported(l *capability.List, reader io.Reader, p sideband.Progress) io.Reader {
	var t sideband.Type

	switch {
	case l.Supports(capability.Sideband):
		t = sideband.Sideband
	case l.Supports(capability.Sideband64k):
		t = sideband.Sideband64k
	default:
		return reader
	}

	d := sideband.NewDemuxer(t, reader)
	d.Progress = p

	return d
}

func updateLocalReferenceStorage(
	s storage.Storer,
	specs []config.RefSpec,
	fetchedRefs, remoteRefs memory.ReferenceStorage,
	specToRefs [][]*plumbing.Reference,
	tagMode plumbing.TagMode,
	force bool,
) (updated bool, err error) {
	isWildcard := true
	forceNeeded := false

	for i, spec := range specs {
		if !spec.IsWildcard() {
			isWildcard = false
		}

		for _, ref := range specToRefs[i] {
			if ref.Type() != plumbing.HashReference {
				continue
			}

			localName := spec.Dst(ref.Name())
			// If localName doesn't start with "refs/" then treat as a branch.
			if !strings.HasPrefix(localName.String(), "refs/") {
				localName = plumbing.NewBranchReferenceName(localName.String())
			}
			old, _ := storer.ResolveReference(s, localName)
			new := plumbing.NewHashReference(localName, ref.Hash())

			// If the ref exists locally as a non-tag and force is not
			// specified, only update if the new ref is an ancestor of the old
			if old != nil && !old.Name().IsTag() && !force && !spec.IsForceUpdate() {
				ff, err := isFastForward(s, old.Hash(), new.Hash())
				if err != nil {
					return updated, err
				}

				if !ff {
					forceNeeded = true
					continue
				}
			}

			refUpdated, err := repository.CheckAndUpdateReferenceStorerIfNeeded(s, new, old)
			if err != nil {
				return updated, err
			}

			if refUpdated {
				updated = true
			}
		}
	}

	if tagMode == plumbing.NoTags {
		return updated, nil
	}

	tags := fetchedRefs
	if isWildcard {
		tags = remoteRefs
	}
	tagUpdated, err := buildFetchedTags(s, tags)
	if err != nil {
		return updated, err
	}

	if tagUpdated {
		updated = true
	}

	if forceNeeded {
		err = ErrForceNeeded
	}

	return
}

func buildFetchedTags(s storage.Storer, refs memory.ReferenceStorage) (updated bool, err error) {
	for _, ref := range refs {
		if !ref.Name().IsTag() {
			continue
		}

		_, err := s.EncodedObject(plumbing.AnyObject, ref.Hash())
		if err == plumbing.ErrObjectNotFound {
			continue
		}

		if err != nil {
			return false, err
		}

		refUpdated, err := repository.CheckAndUpdateReferenceStorerIfNeeded(s, ref, nil)
		if err != nil {
			return updated, err
		}

		if refUpdated {
			updated = true
		}
	}

	return
}

func depthChanged(before []plumbing.Hash, s storage.Storer) (bool, error) {
	after, err := s.Shallow()
	if err != nil {
		return false, err
	}

	if len(before) != len(after) {
		return true, nil
	}

	bm := make(map[plumbing.Hash]bool, len(before))
	for _, b := range before {
		bm[b] = true
	}
	for _, a := range after {
		if _, ok := bm[a]; !ok {
			return true, nil
		}
	}

	return false, nil
}

const refspecAllTags = "+refs/tags/*:refs/tags/*"

func calculateRefs(
	spec []config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	tagMode plumbing.TagMode,
) (memory.ReferenceStorage, [][]*plumbing.Reference, error) {
	if tagMode == plumbing.AllTags {
		spec = append(spec, refspecAllTags)
	}

	refs := make(memory.ReferenceStorage)
	// list of references matched for each spec
	specToRefs := make([][]*plumbing.Reference, len(spec))
	for i := range spec {
		var err error
		specToRefs[i], err = doCalculateRefs(spec[i], remoteRefs, refs)
		if err != nil {
			return nil, nil, err
		}
	}

	return refs, specToRefs, nil
}

func doCalculateRefs(
	s config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	refs memory.ReferenceStorage,
) ([]*plumbing.Reference, error) {
	var refList []*plumbing.Reference

	if s.IsExactSHA1() {
		ref := plumbing.NewHashReference(s.Dst(""), plumbing.NewHash(s.Src()))

		refList = append(refList, ref)
		return refList, refs.SetReference(ref)
	}

	var matched bool
	onMatched := func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.SymbolicReference {
			target, err := storer.ResolveReference(remoteRefs, ref.Name())
			if err != nil {
				return err
			}

			ref = plumbing.NewHashReference(ref.Name(), target.Hash())
		}

		if ref.Type() != plumbing.HashReference {
			return nil
		}

		matched = true
		refList = append(refList, ref)
		return refs.SetReference(ref)
	}

	var ret error
	if s.IsWildcard() {
		iter, err := remoteRefs.IterReferences()
		if err != nil {
			return nil, err
		}
		ret = iter.ForEach(func(ref *plumbing.Reference) error {
			if !s.Match(ref.Name()) {
				return nil
			}

			return onMatched(ref)
		})
	} else {
		var resolvedRef *plumbing.Reference
		src := s.Src()
		resolvedRef, ret = repository.ExpandRef(remoteRefs, plumbing.ReferenceName(src))
		if ret == nil {
			ret = onMatched(resolvedRef)
		}
	}

	if !matched && !s.IsWildcard() {
		return nil, NoMatchingRefSpecError{refSpec: s}
	}

	return refList, ret
}

func getWants(localStorer storage.Storer, refs memory.ReferenceStorage, depth int) ([]plumbing.Hash, error) {
	// If depth is anything other than 1 and the repo has shallow commits then just because we have the commit
	// at the reference doesn't mean that we don't still need to fetch the parents
	shallow := false
	if depth != 1 {
		if s, _ := localStorer.Shallow(); len(s) > 0 {
			shallow = true
		}
	}

	wants := map[plumbing.Hash]bool{}
	for _, ref := range refs {
		hash := ref.Hash()
		exists, err := objectExists(localStorer, ref.Hash())
		if err != nil {
			return nil, err
		}

		if !exists || shallow {
			wants[hash] = true
		}
	}

	var result []plumbing.Hash
	for h := range wants {
		result = append(result, h)
	}

	return result, nil
}

func objectExists(s storer.EncodedObjectStorer, h plumbing.Hash) (bool, error) {
	_, err := s.EncodedObject(plumbing.AnyObject, h)
	if err == plumbing.ErrObjectNotFound {
		return false, nil
	}

	return true, err
}

func checkFastForwardUpdate(s storer.EncodedObjectStorer, remoteRefs storer.ReferenceStorer, cmd *packp.Command) error {
	if cmd.Old == plumbing.ZeroHash {
		_, err := remoteRefs.Reference(cmd.Name)
		if err == plumbing.ErrReferenceNotFound {
			return nil
		}

		if err != nil {
			return err
		}

		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	ff, err := isFastForward(s, cmd.Old, cmd.New)
	if err != nil {
		return err
	}

	if !ff {
		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	return nil
}

func isFastForward(s storer.EncodedObjectStorer, old, new plumbing.Hash) (bool, error) {
	c, err := object.GetCommit(s, new)
	if err != nil {
		return false, err
	}

	found := false
	iter := object.NewCommitPreorderIter(c, nil, nil)
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash != old {
			return nil
		}

		found = true
		return storer.ErrStop
	})
	return found, err
}
