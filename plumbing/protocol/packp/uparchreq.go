package packp

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

// UploadArchiveRequest represents a upload-archive request.
type UploadArchiveRequest struct {
	// Reference is the reference to archive.
	Reference plumbing.ReferenceName
	// Hash is the commit hash to archive.
	Hash plumbing.Hash
	// Format is the format of the archive.
	Format string
	// Prefix is the prefix to strip from each filename in the archive.
	Prefix string
}

// Validate validates the request.
func (r *UploadArchiveRequest) Validate() error {
	if r.Reference == "" && r.Hash.IsZero() {
		return fmt.Errorf("reference or hash is required")
	}

	return nil
}

// NewUploadArchiveRequest creates a new UploadArchiveRequest and returns a pointer.
func NewUploadArchiveRequest() *UploadArchiveRequest {
	return &UploadArchiveRequest{}
}

// Decode reads a upload-archive request from a reader.
func (r *UploadArchiveRequest) Decode(rd io.Reader) error {
	d := newUaReqDecoder(rd)
	return d.Decode(r)
}

type uaReqDecoder struct {
	s     *pktline.Scanner      // a pkt-line scanner from the input stream
	line  []byte                // current pkt-line contents, use parser.nextLine() to make it advance
	nLine int                   // current pkt-line number for debugging, begins at 1
	err   error                 // sticky error, use the parser.error() method to fill this out
	data  *UploadArchiveRequest // parsed data is stored here
}

func newUaReqDecoder(r io.Reader) *uaReqDecoder {
	return &uaReqDecoder{
		s: pktline.NewScanner(r),
	}
}

func (d *uaReqDecoder) Decode(v *UploadArchiveRequest) error {
	d.data = v

	var (
		ref    string
		prefix string
		format string
	)
	args := make([]string, 0)
	for d.s.Scan() {
		d.line = d.s.Bytes()
		d.nLine++

		if !bytes.HasPrefix(d.line, argument) {
			d.err = NewErrUnexpectedData("invalid argument", d.line)
			return d.err
		}

		arg := bytes.Split(d.line, []byte{' '})
		if len(arg) != 2 {
			d.err = NewErrUnexpectedData("missing argument", d.line)
			return d.err
		}

		args = append(args, string(arg[1]))
	}

	if len(args)%2 != 1 {
		d.err = NewErrUnexpectedData("invalid request", d.line)
		return d.err
	}

	for i := 0; i < len(args); i += 2 {
		if i == 0 {
			ref = args[i]
		} else {
			switch args[i] {
			case "--format":
				format = args[i+1]
			case "--prefix":
				prefix = args[i+1]
			default:
				d.err = NewErrUnexpectedData("invalid argument", d.line)
				return d.err
			}
		}
	}

	d.data.Reference = plumbing.ReferenceName(ref)
	d.data.Hash = plumbing.NewHash(ref)
	d.data.Format = format
	d.data.Prefix = prefix

	return d.err
}

// Encode writes a upload-archive request to a writer.
func (r *UploadArchiveRequest) Encode(w io.Writer) error {
	e := newUaReqEncoder(w)
	return e.Encode(r)
}

type uaReqEncoder struct {
	pe *pktline.Encoder
}

func newUaReqEncoder(w io.Writer) *uaReqEncoder {
	return &uaReqEncoder{
		pe: pktline.NewEncoder(w),
	}
}

func (e *uaReqEncoder) Encode(v *UploadArchiveRequest) error {
	if err := v.Validate(); err != nil {
		return err
	}

	if !v.Hash.IsZero() {
		if err := e.pe.Encodef("%s%s\n", argument, v.Hash.String()); err != nil {
			return err
		}
	} else {
		if err := e.pe.Encodef("%s%s\n", argument, v.Reference); err != nil {
			return err
		}
	}

	if v.Format != "" {
		if err := e.pe.Encodef("%s--format\n", argument); err != nil {
			return err
		}
		if err := e.pe.Encodef("%s%s\n", argument, v.Format); err != nil {
			return err
		}
	}

	if v.Prefix != "" {
		if err := e.pe.Encodef("%s--prefix\n", argument); err != nil {
			return err
		}
		if err := e.pe.Encodef("%s%s\n", argument, v.Prefix); err != nil {
			return err
		}
	}

	if err := e.pe.Flush(); err != nil {
		return err
	}

	return nil
}
