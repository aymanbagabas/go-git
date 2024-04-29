package transport

import (
	"context"
	"io"
	"log"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

// buildUpdateRequests constructs a new update-requests object for the given
// connection and push request.
func buildUpdateRequests(caps *capability.List, req *PushRequest) *packp.UpdateRequests {
	upreq := packp.NewUpdateRequests()

	// Prepare the request and capabilities
	if caps.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent()) // nolint: errcheck
	}
	if caps.Supports(capability.ReportStatus) {
		upreq.Capabilities.Set(capability.ReportStatus) // nolint: errcheck
	}
	if req.Progress != nil {
		if caps.Supports(capability.Sideband64k) {
			upreq.Capabilities.Set(capability.Sideband64k) // nolint: errcheck
		} else if caps.Supports(capability.Sideband) {
			upreq.Capabilities.Set(capability.Sideband) // nolint: errcheck
		}
	} else if caps.Supports(capability.NoProgress) {
		upreq.Capabilities.Set(capability.NoProgress) // nolint: errcheck
	}
	if caps.Supports(capability.PushOptions) {
		upreq.Capabilities.Set(capability.PushOptions) // nolint: errcheck
		for k, v := range req.Options {
			upreq.Options = append(upreq.Options, &packp.Option{Key: k, Value: v})
		}
	}
	if req.Atomic && caps.Supports(capability.Atomic) {
		upreq.Capabilities.Set(capability.Atomic) // nolint: errcheck
	}

	upreq.Commands = req.Commands

	return upreq
}

// SendPack is a function that sends a packfile to a remote server.
func SendPack(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	writer io.WriteCloser,
	reader io.ReadCloser,
	req *PushRequest,
) error {
	caps := conn.Capabilities()
	upreq := buildUpdateRequests(caps, req)
	if err := upreq.Encode(writer); err != nil {
		return err
	}

	// Send the packfile.
	if req.Packfile != nil {
		log.Printf("transport: sending packfile to server")
		n, err := io.Copy(writer, req.Packfile)
		if err != nil {
			return err
		}

		log.Printf("transport: sent %d bytes", n)

		if err := req.Packfile.Close(); err != nil {
			return err
		}
	}

	// Close the write pipe to signal the end of the request.
	if err := writer.Close(); err != nil {
		return err
	}

	if !caps.Supports(capability.ReportStatus) {
		// If we don't have report-status, we're done here.
		return nil
	}

	var r io.Reader = reader
	if req.Progress != nil {
		var d *sideband.Demuxer
		if caps.Supports(capability.Sideband64k) {
			d = sideband.NewDemuxer(sideband.Sideband64k, reader)
		} else if caps.Supports(capability.Sideband) {
			d = sideband.NewDemuxer(sideband.Sideband, reader)
		}
		if d != nil {
			d.Progress = req.Progress
			r = d
		}
	}

	report := packp.NewReportStatus()
	if err := report.Decode(r); err != nil {
		return err
	}

	if err := reader.Close(); err != nil {
		return err
	}

	return report.Error()
}
