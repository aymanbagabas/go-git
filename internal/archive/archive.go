// Package archive provides archive generation for git-upload-archive.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

var (
	// ErrListFormats is returned when --list is requested.
	ErrListFormats = errors.New("list formats requested")

	// ErrNotFound is returned when an object, path, or reference cannot be found.
	ErrNotFound = errors.New("not found")

	// ErrSecurity is returned when a treeish expression violates security restrictions
	// (e.g., raw SHA-1 hashes or relative expressions when allowUnreachable is false).
	ErrSecurity = errors.New("security restriction")

	// ErrInvalidArgument is returned when an invalid argument or option is provided.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Formats returns the supported archive formats.
func Formats() []string {
	return []string{"tar", "tar.gz", "tgz", "zip"}
}

// WriteArchive generates an archive from the repository and writes it to w.
//
// Args follow the same format as git-archive: [options...] <tree-ish> [paths...]
// Supported options: --format, --prefix, -l/--list
// Returns ErrListFormats if --list is requested.
func WriteArchive(st storage.Storer, w io.Writer, allowUnreachable bool, args ...string) error {
	format := "tar"
	prefix := ""
	var treeish string
	var paths []string
	list := false

	// Normalize arguments: convert "--format zip" to "--format=zip" etc.
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--format", "--prefix":
			i++
			if i >= len(args) {
				return fmt.Errorf("%w: %s requires an argument", ErrInvalidArgument, arg)
			}
			normalized = append(normalized, arg+"="+args[i])
		default:
			normalized = append(normalized, arg)
		}
	}

	for _, arg := range normalized {
		switch {
		case arg == "--list" || arg == "-l":
			list = true
		case strings.HasPrefix(arg, "--format="):
			format = arg[len("--format="):]
		case strings.HasPrefix(arg, "--prefix="):
			prefix = arg[len("--prefix="):]
		case arg == "--":
			// paths are handled below
		default:
			if !strings.HasPrefix(arg, "-") {
				treeish = arg
			} else {
				return fmt.Errorf("%w: unknown option: %s", ErrInvalidArgument, arg)
			}
		}
	}

	// Extract paths after treeish
	for i, arg := range normalized {
		if arg == treeish {
			if i+1 < len(normalized) {
				if normalized[i+1] == "--" {
					paths = normalized[i+2:]
				} else {
					paths = normalized[i+1:]
				}
			}
			break
		}
	}

	if list {
		return ErrListFormats
	}

	if treeish == "" {
		return fmt.Errorf("%w: no tree-ish specified", ErrInvalidArgument)
	}

	tree, modTime, err := ResolveTreeish(st, treeish, allowUnreachable)
	if err != nil {
		return err
	}

	switch format {
	case "tar":
		return writeTarArchive(st, w, tree, prefix, paths, modTime)
	case "tar.gz", "tgz":
		gw := gzip.NewWriter(w)
		if err := writeTarArchive(st, gw, tree, prefix, paths, modTime); err != nil {
			return err
		}
		return gw.Close()
	case "zip":
		return writeZipArchive(st, w, tree, prefix, paths, modTime)
	default:
		return fmt.Errorf("%w: unsupported archive format: %s", ErrInvalidArgument, format)
	}
}

// defaultUmask is the default tar umask (002).
const defaultUmask = 0o002

// applyUmask applies umask to the given mode for regular files.
// Returns mode with all permission bits set, then applies umask.
func applyUmask(mode int64, isExecutable bool) int64 {
	if isExecutable {
		return (mode | 0o777) &^ defaultUmask
	}
	return (mode | 0o666) &^ defaultUmask
}

// applyUmaskDir applies umask to directories.
// Directories always get full permissions minus umask.
func applyUmaskDir(mode int64) int64 {
	return (mode | 0o777) &^ defaultUmask
}

func writeTarArchive(st storage.Storer, w io.Writer, tree *object.Tree, prefix string, pathFilter []string, modTime time.Time) error {
	tw := tar.NewWriter(w)

	if prefix != "" && strings.HasSuffix(prefix, "/") {
		_ = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     prefix,
			Mode:     applyUmaskDir(0),
			ModTime:  modTime,
		})
	}

	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if len(pathFilter) > 0 && !matchesPathFilter(name, pathFilter) {
			continue
		}

		fullName := prefix + name

		// Extract Unix permission bits from git mode.
		unixMode := int64(entry.Mode) & 0o777

		if entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule {
			_ = tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     fullName + "/",
				Mode:     applyUmaskDir(unixMode),
				ModTime:  modTime,
			})
			continue
		}

		blob, err := object.GetBlob(st, entry.Hash)
		if err != nil {
			return err
		}

		hdr := &tar.Header{
			Name:    fullName,
			Size:    blob.Size,
			Mode:    unixMode,
			ModTime: modTime,
		}

		if entry.Mode == filemode.Symlink {
			rc, err := blob.Reader()
			if err != nil {
				return err
			}
			target, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return err
			}
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = string(target)
			hdr.Size = 0
			// Symlinks always get 0777 per canonical git.
			hdr.Mode = 0o777
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}

		isExec := entry.Mode == filemode.Executable
		hdr.Mode = applyUmask(unixMode, isExec)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		rc, err := blob.Reader()
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}

	return tw.Close()
}

func writeZipArchive(st storage.Storer, w io.Writer, tree *object.Tree, prefix string, pathFilter []string, modTime time.Time) error {
	zw := zip.NewWriter(w)

	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if len(pathFilter) > 0 && !matchesPathFilter(name, pathFilter) {
			continue
		}

		if entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule {
			continue
		}

		fullName := prefix + name
		blob, err := object.GetBlob(st, entry.Hash)
		if err != nil {
			return err
		}

		// Extract Unix permission bits from git mode and apply default umask.
		unixMode := int64(entry.Mode) & 0o777

		fh := &zip.FileHeader{
			Name:     fullName,
			Method:   zip.Deflate,
			Modified: modTime,
		}
		switch entry.Mode {
		case filemode.Executable:
			fh.SetMode(fs.FileMode(applyUmask(unixMode, true)))
		case filemode.Symlink:
			// Zip stores symlinks with mode 0o120000 + permissions.
			fh.SetMode(fs.FileMode(0o120000 | (applyUmask(unixMode, true) & 0o777)))
		default:
			fh.SetMode(fs.FileMode(applyUmask(unixMode, false)))
		}

		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}

		rc, err := blob.Reader()
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}

	return zw.Close()
}

func matchesPathFilter(name string, filters []string) bool {
	for _, f := range filters {
		if name == f || strings.HasPrefix(name, f+"/") || strings.HasPrefix(f, name+"/") {
			return true
		}
		if matched, _ := filepath.Match(f, name); matched {
			return true
		}
	}
	return false
}

// ResolveTreeish resolves a tree-ish expression to a tree object.
//
// Security: By default, only direct ref names (v1.0, main) and ref:path
// sub-tree syntax (v1.0:Documentation) are allowed. Raw SHA-1 hashes and
// relative expressions (main^, HEAD~2) are rejected unless
// allowUnreachable is true.
func ResolveTreeish(st storage.Storer, treeish string, allowUnreachable bool) (*object.Tree, time.Time, error) {
	var subPath string
	if idx := strings.IndexByte(treeish, ':'); idx >= 0 {
		subPath = treeish[idx+1:]
		treeish = treeish[:idx]
	}

	if !allowUnreachable {
		if plumbing.IsHash(treeish) {
			return nil, time.Time{}, fmt.Errorf("%w: only ref names are allowed (got %s)", ErrSecurity, treeish)
		}
		if strings.ContainsAny(treeish, "^~@{}") {
			return nil, time.Time{}, fmt.Errorf("%w: relative expressions are not allowed (got %s)", ErrSecurity, treeish)
		}
	}

	h, err := resolveRef(st, treeish, allowUnreachable)
	if err != nil {
		return nil, time.Time{}, err
	}

	obj, err := object.GetObject(st, h)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: object not found: %s", ErrNotFound, treeish)
	}

	var commitTime time.Time
	var tree *object.Tree

	switch o := obj.(type) {
	case *object.Commit:
		commitTime = o.Committer.When
		tree, err = o.Tree()
		if err != nil {
			return nil, time.Time{}, err
		}
	case *object.Tag:
		commit, err := object.GetCommit(st, o.Target)
		if err != nil {
			return nil, time.Time{}, err
		}
		commitTime = commit.Committer.When
		tree, err = commit.Tree()
		if err != nil {
			return nil, time.Time{}, err
		}
	case *object.Tree:
		tree = o
		commitTime = time.Now()
	default:
		return nil, time.Time{}, fmt.Errorf("%w: unsupported object type for archive: %T", ErrInvalidArgument, obj)
	}

	if subPath != "" {
		entry, err := tree.FindEntry(subPath)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("%w: path not found in tree: %s", ErrNotFound, subPath)
		}
		if entry.Mode != filemode.Dir {
			return nil, time.Time{}, fmt.Errorf("%w: path is not a directory: %s", ErrInvalidArgument, subPath)
		}
		tree, err = object.GetTree(st, entry.Hash)
		if err != nil {
			return nil, time.Time{}, err
		}
	}

	return tree, commitTime, nil
}

func resolveRef(st storage.Storer, name string, allowHash bool) (plumbing.Hash, error) {
	if allowHash && plumbing.IsHash(name) {
		return plumbing.NewHash(name), nil
	}

	// Try as reference
	for _, candidate := range []plumbing.ReferenceName{
		plumbing.ReferenceName(name),
		plumbing.ReferenceName("refs/heads/" + name),
		plumbing.ReferenceName("refs/tags/" + name),
	} {
		ref, err := storer.ResolveReference(st, candidate)
		if err == nil {
			return ref.Hash(), nil
		}
	}

	return plumbing.ZeroHash, fmt.Errorf("%w: cannot resolve %q", ErrNotFound, name)
}
