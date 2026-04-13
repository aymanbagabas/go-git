package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/internal/archive"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// UploadArchiveRequest configures the server-side upload-archive service.
type UploadArchiveRequest struct {
	// AllowUnreachable when true disables the default security restrictions
	// and allows clients to use arbitrary SHA-1 expressions. By default,
	// only direct ref targets (e.g. v1.0, main) and ref:path sub-tree
	// syntax (e.g. v1.0:Documentation) are allowed.
	//
	// This serves as the default value. When the repository-level config
	// uploadArchive.allowUnreachable is explicitly set, it always overrides
	// this value (both true → false and false → true).
	// See https://git-scm.com/docs/git-upload-archive
	AllowUnreachable bool
}

// UploadArchive is a server command that serves the git-upload-archive service.
//
// It reads argument pkt-lines from r, sends ACK + flush, then generates the
// archive and streams it to w using sideband multiplexing.
//
// Wire protocol:
//
//	Client → Server: "argument <arg>\n" pkt-lines + flush
//	Server → Client: "ACK\n" pkt-line + flush
//	Server → Client: sideband packets (band 1 = archive data, band 2 = progress)
func UploadArchive(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	req *UploadArchiveRequest,
) error {
	if req == nil {
		req = &UploadArchiveRequest{}
	}

	w = ioutil.NewContextWriteCloser(ctx, w)

	allowUnreachable := req.AllowUnreachable
	if v, ok := readAllowUnreachable(st); ok {
		allowUnreachable = v
	}

	args, err := readArchiveArgs(r)
	if err != nil {
		writeNACK(w, err.Error())
		return err
	}

	if _, err := pktline.WriteString(w, "ACK\n"); err != nil {
		return fmt.Errorf("upload-archive: writing ACK: %w", err)
	}
	if err := pktline.WriteFlush(w); err != nil {
		return fmt.Errorf("upload-archive: writing flush: %w", err)
	}

	mux := sideband.NewMuxer(sideband.Sideband64k, w)

	err = archive.WriteArchive(st, mux, allowUnreachable, args...)
	if errors.Is(err, archive.ErrListFormats) {
		// List formats and return success
		for _, f := range archive.Formats() {
			if _, err := fmt.Fprintf(mux, "%s\n", f); err != nil {
				return err
			}
		}
		return pktline.WriteFlush(w)
	}
	if err != nil {
		errMsg := fmt.Sprintf("upload-archive: %s", err.Error())
		_, _ = mux.WriteChannel(sideband.ErrorMessage, []byte(errMsg))
		_ = pktline.WriteFlush(w)
		return err
	}

	return pktline.WriteFlush(w)
}

const maxArchiveArgs = 64

// readArchiveArgs reads "argument <arg>\n" pkt-lines until flush.
func readArchiveArgs(r io.Reader) ([]string, error) {
	var args []string
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return nil, fmt.Errorf("upload-archive: reading argument: %w", err)
		}
		if l == pktline.Flush {
			break
		}
		if len(args) >= maxArchiveArgs {
			return nil, fmt.Errorf("upload-archive: too many arguments (>%d)", maxArchiveArgs)
		}

		s := strings.TrimSuffix(string(line), "\n")
		if !strings.HasPrefix(s, "argument ") {
			return nil, fmt.Errorf("upload-archive: expected 'argument' token, got: %s", s)
		}
		args = append(args, s[len("argument "):])
	}
	return args, nil
}

func writeNACK(w io.Writer, reason string) {
	_, _ = pktline.WriteString(w, fmt.Sprintf("NACK %s\n", reason))
	_ = pktline.WriteFlush(w)
}

// readAllowUnreachable reads the uploadArchive.allowUnreachable config
// from the repository. It returns the value and whether the key was
// explicitly set. When the key is absent or the config cannot be read,
// ok is false and the caller should fall back to its own default.
func readAllowUnreachable(st storage.Storer) (value, ok bool) {
	cfg, err := st.Config()
	if err != nil {
		return false, false
	}
	if cfg.UploadArchive.AllowUnreachable.IsSet() {
		return cfg.UploadArchive.AllowUnreachable.IsTrue(), true
	}
	return false, false
}
