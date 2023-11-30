package packp

import (
	"errors"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
)

var (
	ErrEmptyCommands    = errors.New("commands cannot be empty")
	ErrMalformedCommand = errors.New("malformed command")
)

// UpdateRequests values represent reference upload requests.
// Values from this type are not zero-value safe, use the New function instead.
// See https://git-scm.com/docs/pack-protocol#_reference_update_request_and_packfile_transfer
type UpdateRequests struct {
	Capabilities *capability.List
	Commands     []*Command
	Options      []*Option
	Shallow      *plumbing.Hash
}

// New returns a pointer to a new UpdateRequests value.
func NewUpdateRequests() *UpdateRequests {
	return &UpdateRequests{
		// TODO: Add support for push-cert
		Capabilities: capability.NewList(),
		Commands:     nil,
	}
}

// NewUpdateRequestsFromCapabilities returns a pointer to a new
// UpdateRequests value, the request capabilities are filled with the
// most optimal ones, based on the adv value (advertised capabilities), the
// UpdateRequests contains no commands
//
// It does set the following capabilities:
//   - agent
//   - report-status
//   - ofs-delta
//   - ref-delta
//   - delete-refs
//
// It leaves up to the user to add the following capabilities later:
//   - atomic
//   - ofs-delta
//   - side-band
//   - side-band-64k
//   - quiet
//   - push-cert
func NewUpdateRequestsFromCapabilities(adv *capability.List) *UpdateRequests {
	r := NewUpdateRequests()

	if adv.Supports(capability.Agent) {
		r.Capabilities.Set(capability.Agent, capability.DefaultAgent())
	}

	if adv.Supports(capability.ReportStatus) {
		r.Capabilities.Set(capability.ReportStatus)
	}

	return r
}

func (req *UpdateRequests) validate() error {
	if len(req.Commands) == 0 {
		return ErrEmptyCommands
	}

	for _, c := range req.Commands {
		if err := c.validate(); err != nil {
			return err
		}
	}

	return nil
}

type Action string

const (
	Create  Action = "create"
	Update  Action = "update"
	Delete  Action = "delete"
	Invalid Action = "invalid"
)

type Command struct {
	Name plumbing.ReferenceName
	Old  plumbing.Hash
	New  plumbing.Hash
}

func (c *Command) Action() Action {
	if c.Old == plumbing.ZeroHash && c.New == plumbing.ZeroHash {
		return Invalid
	}

	if c.Old == plumbing.ZeroHash {
		return Create
	}

	if c.New == plumbing.ZeroHash {
		return Delete
	}

	return Update
}

func (c *Command) validate() error {
	if c.Action() == Invalid {
		return ErrMalformedCommand
	}

	return nil
}

type Option struct {
	Key   string
	Value string
}
