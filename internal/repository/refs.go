package repository

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
)

const (
	// This describes the maximum number of commits to walk when
	// computing the haves to send to a server, for each ref in the
	// repo containing this remote, when not using the multi-ack
	// protocol.  Setting this to 0 means there is no limit.
	maxHavesToVisitPerRef = 100
)

func ExpandRef(s storer.ReferenceStorer, ref plumbing.ReferenceName) (*plumbing.Reference, error) {
	// For improving troubleshooting, this preserves the error for the provided `ref`,
	// and returns the error for that specific ref in case all parse rules fails.
	var ret error
	for _, rule := range plumbing.RefRevParseRules {
		resolvedRef, err := storer.ResolveReference(s, plumbing.ReferenceName(fmt.Sprintf(rule, ref)))

		if err == nil {
			return resolvedRef, nil
		} else if ret == nil {
			ret = err
		}
	}

	return nil, ret
}

func References(s storer.Storer) ([]*plumbing.Reference, error) {
	var localRefs []*plumbing.Reference

	iter, err := s.IterReferences()
	if err != nil {
		return nil, err
	}

	for {
		ref, err := iter.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		localRefs = append(localRefs, ref)
	}

	return localRefs, nil
}

func UpdateShallow(s storage.Storer, depth int, shallows []plumbing.Hash) error {
	if depth == 0 || len(shallows) == 0 {
		return nil
	}

	shas, err := s.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range shallows {
		for _, oldS := range shas {
			if s == oldS {
				continue outer
			}
		}
		shas = append(shas, s)
	}

	return s.SetShallow(shas)
}

func CheckAndUpdateReferenceStorerIfNeeded(
	s storer.ReferenceStorer, r, old *plumbing.Reference) (
	updated bool, err error) {
	p, err := s.Reference(r.Name())
	if err != nil && err != plumbing.ErrReferenceNotFound {
		return false, err
	}

	// we use the string method to compare references, is the easiest way
	if err == plumbing.ErrReferenceNotFound || r.String() != p.String() {
		if err := s.CheckAndSetReference(r, old); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

func GetHaves(
	localRefs []*plumbing.Reference,
	remoteRefStorer storer.ReferenceStorer,
	s storage.Storer,
	depth int,
) ([]plumbing.Hash, error) {
	haves := map[plumbing.Hash]bool{}

	// Build a map of all the remote references, to avoid loading too
	// many parent commits for references we know don't need to be
	// transferred.
	remoteRefs, err := GetRemoteRefsFromStorer(remoteRefStorer)
	if err != nil {
		return nil, err
	}

	for _, ref := range localRefs {
		if haves[ref.Hash()] {
			continue
		}

		if ref.Type() != plumbing.HashReference {
			continue
		}

		err = GetHavesFromRef(ref, remoteRefs, s, haves, depth)
		if err != nil {
			return nil, err
		}
	}

	var result []plumbing.Hash
	for h := range haves {
		result = append(result, h)
	}

	return result, nil
}

func GetRemoteRefsFromStorer(remoteRefStorer storer.ReferenceStorer) (
	map[plumbing.Hash]bool, error) {
	remoteRefs := map[plumbing.Hash]bool{}
	iter, err := remoteRefStorer.IterReferences()
	if err != nil {
		return nil, err
	}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}
		remoteRefs[ref.Hash()] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return remoteRefs, nil
}

// GetHavesFromRef populates the given `haves` map with the given
// reference, and up to `maxHavesToVisitPerRef` ancestor commits.
func GetHavesFromRef(
	ref *plumbing.Reference,
	remoteRefs map[plumbing.Hash]bool,
	s storage.Storer,
	haves map[plumbing.Hash]bool,
	depth int,
) error {
	h := ref.Hash()
	if haves[h] {
		return nil
	}

	// No need to load the commit if we know the remote already
	// has this hash.
	if remoteRefs[h] {
		haves[h] = true
		return nil
	}

	commit, err := object.GetCommit(s, h)
	if err != nil {
		// Ignore the error if this isn't a commit.
		haves[ref.Hash()] = true
		return nil
	}

	// Until go-git supports proper commit negotiation during an
	// upload pack request, include up to `maxHavesToVisitPerRef`
	// commits from the history of each ref.
	walker := object.NewCommitPreorderIter(commit, haves, nil)
	toVisit := maxHavesToVisitPerRef
	// But only need up to the requested depth
	if depth > 0 && depth < maxHavesToVisitPerRef {
		toVisit = depth
	}
	// It is safe to ignore any error here as we are just trying to find the references that we already have
	// An example of a legitimate failure is we have a shallow clone and don't have the previous commit(s)
	_ = walker.ForEach(func(c *object.Commit) error {
		haves[c.Hash] = true
		toVisit--
		// If toVisit starts out at 0 (indicating there is no
		// max), then it will be negative here and we won't stop
		// early.
		if toVisit == 0 || remoteRefs[c.Hash] {
			return storer.ErrStop
		}
		return nil
	})

	return nil
}
