package git

import (
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v6/storage"
)

// ArchiveOptions are options for writing a repository archive from a named
// tree.
type ArchiveOptions struct {
	// Format is the archive format. The default is "tar".
	Format string
	// Remote is the remote repository to archive. This is exclusive with the
	// Storer option.
	Remote string
	// Storer is the [storage.Storer] to use for the archive. This is
	// exclusive with the Remote option.
	Storer storage.Storer
	// Tree is the tree to archive. This is required.
	Tree string
	// Paths is an optional list of paths to include in the archive. If not
	// specified, all files in the tree will be included.
	Paths []string
}

// Validate validates the archive options.
func (o *ArchiveOptions) Validate() error {
	if o.Format == "" {
		o.Format = "tar"
	}
	if o.Remote != "" && o.Storer != nil {
		return errors.New("cannot specify both remote and storer")
	}
	if o.Storer == nil && o.Remote == "" {
		return errors.New("must specify either remote or storer")
	}
	return nil
}

// ArchiveContext writes an archive of files from the repository to the writer.
func ArchiveContext(ctx context.Context, w io.Writer, o *ArchiveOptions) error {
	return nil
}

// Archive writes an archive of files from the repository to the writer.
func Archive(w io.Writer, o *ArchiveOptions) error {
	return ArchiveContext(context.Background(), w, o)
}
