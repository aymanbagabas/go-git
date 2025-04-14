package archive

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/storage"
)

// Archiver is an interface for writing an archive.
type Archiver interface {
	// Archive writes the archive to the writer.
	Archive(ctx context.Context, w io.Writer, st storage.Storer) error
}
