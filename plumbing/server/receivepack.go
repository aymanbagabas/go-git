package server

import (
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

var (
	ErrUpdateReference = errors.New("failed to update ref")
)

type rpStatus struct {
	cmdStatus map[plumbing.ReferenceName]error
	firstErr  error
	unpackErr error
}

func ReceivePack(ctx context.Context, st storer.Storer, req *packp.ReferenceUpdateRequest) (*packp.ReportStatus, error) {
	//TODO: Implement 'atomic' update of references.

	s := rpStatus{
		cmdStatus: make(map[plumbing.ReferenceName]error),
	}
	if req.Packfile != nil {
		r := ioutil.NewContextReadCloser(ctx, req.Packfile)
		if err := writePackfile(st, r); err != nil {
			s.unpackErr = err
			s.firstErr = err
			return reportStatus(s), err
		}
	}

	updateReferences(&s, st, req)
	return reportStatus(s), s.firstErr
}

func updateReferences(s *rpStatus, st storer.Storer, req *packp.ReferenceUpdateRequest) {
	for _, cmd := range req.Commands {
		exists, err := referenceExists(st, cmd.Name)
		if err != nil {
			setStatus(s, cmd.Name, err)
			continue
		}

		switch cmd.Action() {
		case packp.Create:
			if exists {
				setStatus(s, cmd.Name, ErrUpdateReference)
				continue
			}

			ref := plumbing.NewHashReference(cmd.Name, cmd.New)
			err := st.SetReference(ref)
			setStatus(s, cmd.Name, err)
		case packp.Delete:
			if !exists {
				setStatus(s, cmd.Name, ErrUpdateReference)
				continue
			}

			err := st.RemoveReference(cmd.Name)
			setStatus(s, cmd.Name, err)
		case packp.Update:
			if !exists {
				setStatus(s, cmd.Name, ErrUpdateReference)
				continue
			}

			ref := plumbing.NewHashReference(cmd.Name, cmd.New)
			err := st.SetReference(ref)
			setStatus(s, cmd.Name, err)
		}
	}
}

func writePackfile(st storer.Storer, r io.ReadCloser) error {
	if r == nil {
		return nil
	}

	if err := packfile.UpdateObjectStorage(st, r); err != nil {
		_ = r.Close()
		return err
	}

	return r.Close()
}

func setStatus(s *rpStatus, ref plumbing.ReferenceName, err error) {
	s.cmdStatus[ref] = err
	if s.firstErr == nil && err != nil {
		s.firstErr = err
	}
}

func reportStatus(s rpStatus) *packp.ReportStatus {
	rs := packp.NewReportStatus()
	rs.UnpackStatus = "ok"

	if s.unpackErr != nil {
		rs.UnpackStatus = s.unpackErr.Error()
	}

	if s.cmdStatus == nil {
		return rs
	}

	for ref, err := range s.cmdStatus {
		msg := "ok"
		if err != nil {
			msg = err.Error()
		}
		status := &packp.CommandStatus{
			ReferenceName: ref,
			Status:        msg,
		}
		rs.CommandStatuses = append(rs.CommandStatuses, status)
	}

	return rs
}
