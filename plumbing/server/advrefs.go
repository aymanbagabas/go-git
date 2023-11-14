package server

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func advertiseReferences(st storer.Storer, forPush bool) (*packp.AdvRefs, error) {
	ar := packp.NewAdvRefs()

	if err := setSupportedCapabilities(ar.Capabilities, forPush); err != nil {
		return nil, err
	}

	if err := setReferences(st, ar); err != nil {
		return nil, err
	}

	if err := setHEAD(st, ar); err != nil {
		return nil, err
	}

	if !forPush && len(ar.References) == 0 {
		return nil, transport.ErrEmptyRemoteRepository
	}

	return ar, nil
}

func setSupportedCapabilities(c *capability.List, forPush bool) error {
	type capv struct {
		c capability.Capability
		v []string
	}
	caps := []capv{
		{capability.Agent, []string{capability.DefaultAgent()}},
		{capability.OFSDelta, nil},
	}

	if forPush {
		caps = append(caps,
			capv{capability.DeleteRefs, nil},
			capv{capability.ReportStatus, nil},
		)
	}

	for _, cap := range caps {
		if err := c.Set(cap.c, cap.v...); err != nil {
			return err
		}
	}

	return nil
}

func setHEAD(s storer.Storer, ar *packp.AdvRefs) error {
	ref, err := s.Reference(plumbing.HEAD)
	if err == plumbing.ErrReferenceNotFound {
		return nil
	}

	if err != nil {
		return err
	}

	if ref.Type() == plumbing.SymbolicReference {
		if err := ar.AddReference(ref); err != nil {
			return nil
		}

		ref, err = storer.ResolveReference(s, ref.Target())
		if err == plumbing.ErrReferenceNotFound {
			return nil
		}

		if err != nil {
			return err
		}
	}

	if ref.Type() != plumbing.HashReference {
		return plumbing.ErrInvalidType
	}

	h := ref.Hash()
	ar.Head = &h

	return nil
}

func setReferences(s storer.Storer, ar *packp.AdvRefs) error {
	//TODO: add peeled references.
	iter, err := s.IterReferences()
	if err != nil {
		return err
	}

	return iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		ar.References[ref.Name().String()] = ref.Hash()
		return nil
	})
}

func referenceExists(s storer.ReferenceStorer, n plumbing.ReferenceName) (bool, error) {
	_, err := s.Reference(n)
	if err == plumbing.ErrReferenceNotFound {
		return false, nil
	}

	return err == nil, err
}

func checkSupportedCapabilities(cur, req *capability.List) error {
	for _, c := range req.All() {
		if !cur.Supports(c) {
			return fmt.Errorf("unsupported capability: %s", c)
		}
	}

	return nil
}
