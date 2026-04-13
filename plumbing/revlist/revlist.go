// Package revlist provides support to access the ancestors of commits, in a
// similar way as the git-rev-list command.
package revlist

import (
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// objectWalker can be implemented by storers that provide a specialized
// revlist object walk for a wants/haves query.
type objectWalker interface {
	RevListObjects(wants, haves []plumbing.Hash) ([]plumbing.Hash, error)
}

// Objects computes object hashes reachable from wants while excluding
// commits reachable from haves.
//
// If s implements objectWalker, its RevListObjects method is used.
// Otherwise, Objects expands haves first to establish commit boundaries,
// then walks wants in the same object store.
func Objects(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) ([]plumbing.Hash, error) {
	return ObjectsProgress(s, wants, haves, nil)
}

// ObjectsProgress computes object hashes reachable from wants while excluding
// commits reachable from haves. If progress is not nil, it will be used to
// report progress messages during object enumeration.
func ObjectsProgress(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
	progress sideband.Progress,
) ([]plumbing.Hash, error) {
	if walker, ok := s.(objectWalker); ok {
		return walker.RevListObjects(wants, haves)
	}

	w, err := newObjectWalk(s)
	if err != nil {
		return nil, err
	}
	if err := w.seedHaves(haves); err != nil {
		return nil, err
	}
	if err := w.seedWants(wants); err != nil {
		return nil, err
	}
	if err := w.walk(); err != nil {
		return nil, err
	}
	if progress != nil {
		_, _ = fmt.Fprintf(progress, "Enumerating objects: %d, done.\n", len(w.result))
	}
	return w.result, nil
}

// ObjectsWithRef returns a map from each reachable object hash to the
// list of want hashes that can reach it.
func ObjectsWithRef(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
) (map[plumbing.Hash][]plumbing.Hash, error) {
	return ObjectsWithRefProgress(s, wants, haves, nil)
}

// ObjectsWithRefProgress returns a map from each reachable object hash to the
// list of want hashes that can reach it. If progress is not nil, it will be
// used to report progress messages during object enumeration.
func ObjectsWithRefProgress(
	s storer.EncodedObjectStorer,
	wants,
	haves []plumbing.Hash,
	progress sideband.Progress,
) (map[plumbing.Hash][]plumbing.Hash, error) {
	all := map[plumbing.Hash][]plumbing.Hash{}
	totalWants := len(wants)
	for i, want := range wants {
		hashes, err := Objects(s, []plumbing.Hash{want}, haves)
		if err != nil {
			return nil, err
		}
		for _, h := range hashes {
			all[h] = append(all[h], want)
		}
		if progress != nil {
			_, _ = fmt.Fprintf(progress, "Enumerating objects: %d%% (%d/%d)\r", (i+1)*100/totalWants, i+1, totalWants)
		}
	}
	return all, nil
}
