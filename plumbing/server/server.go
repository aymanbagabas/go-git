package server

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

// ServerCommand is used for a single server command execution.
type ServerCommand struct {
	Stderr io.Writer
	Stdout io.WriteCloser
	Stdin  io.Reader
}

func ServeUploadPack(ctx context.Context, cmd ServerCommand, st storer.Storer) (err error) {
	defer ioutil.CheckClose(cmd.Stdout, &err)

	// Supported capabilities by the server.
	ar, err := advertiseReferences(st, false)
	if err != nil {
		return err
	}

	if err := ar.Encode(cmd.Stdout); err != nil {
		return err
	}

	req := packp.NewUploadPackRequest()
	if err := req.Decode(cmd.Stdin); err != nil {
		return err
	}

	if req.IsEmpty() {
		return transport.ErrEmptyUploadPackRequest
	}

	if err := req.Validate(); err != nil {
		return err
	}

	if len(req.Shallows) > 0 {
		// TODO implement shallow
		return fmt.Errorf("shallow not supported")
	}

	// Check if the server supports the capabilities requested by the client.
	if err := checkSupportedCapabilities(ar.Capabilities, req.Capabilities); err != nil {
		return err
	}

	resp, err := UploadPack(ctx, st, req)
	if err != nil {
		return err
	}

	return resp.Encode(cmd.Stdout)
}

func ServeReceivePack(ctx context.Context, cmd ServerCommand, st storer.Storer) error {
	// Supported capabilities by the server.
	ar, err := advertiseReferences(st, true)
	if err != nil {
		return fmt.Errorf("internal error in advertised references: %s", err)
	}

	if err := ar.Encode(cmd.Stdout); err != nil {
		return fmt.Errorf("error in advertised references encoding: %s", err)
	}

	req := packp.NewReferenceUpdateRequest()
	if err := req.Decode(cmd.Stdin); err != nil {
		return fmt.Errorf("error decoding: %s", err)
	}

	// Check if the server supports the capabilities requested by the client.
	if err := checkSupportedCapabilities(ar.Capabilities, req.Capabilities); err != nil {
		return err
	}

	rs, err := ReceivePack(ctx, st, req)
	if rs != nil {
		if err := rs.Encode(cmd.Stdout); err != nil {
			return fmt.Errorf("error in encoding report status %s", err)
		}
	}

	if err != nil {
		return fmt.Errorf("error in receive pack: %s", err)
	}

	return nil
}
