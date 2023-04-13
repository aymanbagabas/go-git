package packp

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"io"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

// UploadArchiveResponse represents a upload-archive response.
type UploadArchiveResponse struct {
	req  *UploadArchiveRequest
	data io.ReadCloser
}

// NewUploadArchiveResponse creates a new UploadArchiveResponse instance.
// The archive data is stored in r.
func NewUploadArchiveResponse(req *UploadArchiveRequest) *UploadArchiveResponse {
	return &UploadArchiveResponse{
		req: req,
	}
}

// Decode decodes the response.
// upload-archive response starts with an ACK packet, followed by a flush packet.
// Then the archive data is sent.
// And finally, a flush packet is sent.
func (r *UploadArchiveResponse) Decode(rd io.ReadCloser) error {
	s := pktline.NewScanner(rd)

	// ACK packet
	if !s.Scan() || !bytes.Equal(s.Bytes(), ack) {
		return NewErrUnexpectedData("unexpected packet", s.Bytes())
	}

	// flush packet
	if !s.Scan() || !bytes.Equal(s.Bytes(), pktline.FlushPkt) {
		return NewErrUnexpectedData("unexpected packet", s.Bytes())
	}

	// archive data
	pr, pw := io.Pipe()
	go func() {
		for s.Scan() {
			pw.Write(s.Bytes())
		}
	}()

	switch r.req.Format {
	case "tar":
		tr := tar.NewReader(pr)
		r.data = io.NopCloser(bufio.NewReader(tr))
	case "zip":
		r.data = pr
	case "tgz", "tar.gz":
		gz, err := gzip.NewReader(pr)
		if err != nil {
			return err
		}

		tr := tar.NewReader(gz)
		r.data = io.NopCloser(bufio.NewReader(tr))
	}

	return s.Err()
}

// Encode encodes the response.
// upload-archive response starts with an ACK packet, followed by a flush packet.
// Then the archive data is sent.
// And finally, a flush packet is sent.
func (r *UploadArchiveResponse) Encode(w io.Writer) error {
	pe := pktline.NewEncoder(w)

	// ACK packet
	if err := pe.Encode(ack); err != nil {
		return err
	}

	// flush packet
	if err := pe.Flush(); err != nil {
		return err
	}

	// archive data
	rdr := bufio.NewReader(r.data)
	for {
		var buf [pktline.MaxPayloadSize]byte
		n, err := rdr.Read(buf[:])
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		if err := pe.Encode(buf[:n]); err != nil {
			return err
		}
	}

	return pe.Flush()
}
