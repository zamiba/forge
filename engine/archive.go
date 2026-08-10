package engine

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	sevenzip "github.com/bodgit/sevenzip"
)

// Extract unpacks an archive into destDir, choosing the format from the file
// extension: .tar.gz/.tgz, .7z, and zip for everything else. destDir is created
// if it does not exist.
func Extract(src, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	switch lower := strings.ToLower(src); {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(src, destDir)
	case strings.HasSuffix(lower, ".7z"):
		return extract7z(src, destDir)
	default:
		return extractZip(src, destDir)
	}
}

// safeJoin joins name onto destDir and rejects any result that escapes it,
// guarding against archives containing "../" entries.
func safeJoin(destDir, name, kind string) (string, error) {
	destPath := filepath.Join(destDir, name)
	rel, err := filepath.Rel(destDir, destPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path in %s: %s", kind, name)
	}
	return destPath, nil
}

// archiveEntry is the shape zip and 7z readers have in common.
type archiveEntry struct {
	name  string
	isDir bool
	mode  os.FileMode
	open  func() (io.ReadCloser, error)
}

func extractEntries(destDir, kind string, entries []archiveEntry) error {
	destDir = filepath.Clean(destDir)
	for _, e := range entries {
		destPath, err := safeJoin(destDir, e.name, kind)
		if err != nil {
			return err
		}
		if e.isDir {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		if err := writeEntry(destPath, e); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(destPath string, e archiveEntry) error {
	rc, err := e.open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, e.mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	entries := make([]archiveEntry, 0, len(r.File))
	for _, f := range r.File {
		entries = append(entries, archiveEntry{
			name:  f.Name,
			isDir: f.FileInfo().IsDir(),
			mode:  f.Mode(),
			open:  func() (io.ReadCloser, error) { return f.Open() },
		})
	}
	return extractEntries(destDir, "zip", entries)
}

func extract7z(src, destDir string) error {
	r, err := sevenzip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	entries := make([]archiveEntry, 0, len(r.File))
	for _, f := range r.File {
		entries = append(entries, archiveEntry{
			name:  f.Name,
			isDir: f.FileInfo().IsDir(),
			mode:  f.Mode(),
			open:  func() (io.ReadCloser, error) { return f.Open() },
		})
	}
	return extractEntries(destDir, "7z", entries)
}

func extractTarGz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	destDir = filepath.Clean(destDir)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		destPath, err := safeJoin(destDir, hdr.Name, "tar")
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Reject links that would point outside the extraction root; a
			// later step reading through such a link would escape the tree.
			if _, err := safeJoin(destDir, filepath.Join(filepath.Dir(hdr.Name), hdr.Linkname), "tar"); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, destPath); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
}
