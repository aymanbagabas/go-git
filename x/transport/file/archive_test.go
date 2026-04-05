package file

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	transport "github.com/go-git/go-git/v6/x/transport"
)

func archiveSession(t *testing.T) transport.Session {
	t.Helper()
	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: repoPath},
		Command: transport.UploadArchiveService,
	})
	require.NoError(t, err)
	return session
}

func TestArchive_Tar(t *testing.T) {
	t.Parallel()

	session := archiveSession(t)
	defer session.Close()

	a, ok := session.(transport.Archivable)
	require.True(t, ok)

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "master"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Greater(t, len(data), 0)

	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}

func TestArchive_TarGz(t *testing.T) {
	t.Parallel()

	session := archiveSession(t)
	defer session.Close()

	a, ok := session.(transport.Archivable)
	require.True(t, ok)

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar.gz", "master"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	gr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)

	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}

func TestArchive_Zip(t *testing.T) {
	t.Parallel()

	session := archiveSession(t)
	defer session.Close()

	a, ok := session.(transport.Archivable)
	require.True(t, ok)

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=zip", "master"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Greater(t, len(zr.File), 0)
}

func TestArchive_Prefix(t *testing.T) {
	t.Parallel()

	session := archiveSession(t)
	defer session.Close()

	a, ok := session.(transport.Archivable)
	require.True(t, ok)

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "--prefix=myproject/", "master"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(hdr.Name, "myproject/"), "expected prefix myproject/, got %s", hdr.Name)
	}
}

func TestArchive_List(t *testing.T) {
	t.Parallel()

	session := archiveSession(t)
	defer session.Close()

	a, ok := session.(transport.Archivable)
	require.True(t, ok)

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--list"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	formats := strings.TrimSpace(string(data))
	lines := strings.Split(formats, "\n")
	assert.Contains(t, lines, "tar")
	assert.Contains(t, lines, "zip")
	assert.Contains(t, lines, "tar.gz")
	assert.Contains(t, lines, "tgz")
}

func TestArchive_NotArchivable(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: repoPath},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	a, ok := session.(transport.Archivable)
	require.True(t, ok)

	_, err = a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"master"},
	})
	require.ErrorIs(t, err, transport.ErrArchiveUnsupported)
}
