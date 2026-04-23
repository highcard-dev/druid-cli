package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func RemoveContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func MoveContents(src, dest string) error {
	d, err := os.Open(src)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.Rename(filepath.Join(src, name), filepath.Join(dest, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func CopyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get source file info for permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	//ensure the destination directory exists
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return err
	}

	// Create destination file with same permissions
	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy file contents
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	// Sync to ensure data is written
	if err := destFile.Sync(); err != nil {
		return err
	}

	// Set same permissions as source
	return os.Chmod(dest, srcInfo.Mode())
}

// AutoChunkDataDir returns the list of chunks to pack for the data directory.
//
// Auto mode (explicitChunks is empty): reads top-level entries of dataDir.
// If any entry is a symlink, the entire data/ directory is returned as a
// single chunk (Path: ".") so that ORAS can extract symlinks whose targets
// are siblings under data/. Without symlinks, one chunk per entry is returned.
//
// Explicit mode (explicitChunks provided): validates that no symlink inside
// a chunk's directory tree points into a different chunk's tree (cross-chunk
// symlinks cannot be extracted by ORAS). Returns an error if detected.
func AutoChunkDataDir(dataDir string, explicitChunks []*domain.Chunks) ([]*domain.Chunks, error) {
	if len(explicitChunks) > 0 {
		return validateExplicitChunks(dataDir, explicitChunks)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	hasSymlink := false
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			hasSymlink = true
			break
		}
	}

	if hasSymlink {
		return []*domain.Chunks{{Name: "data", Path: "."}}, nil
	}

	chunks := make([]*domain.Chunks, 0, len(entries))
	for _, entry := range entries {
		chunks = append(chunks, &domain.Chunks{
			Name: entry.Name(),
			Path: entry.Name(),
		})
	}
	return chunks, nil
}

// validateExplicitChunks checks that no symlink inside a chunk's directory
// tree points into a different chunk's tree. Returns an error describing
// the offending symlink if a cross-chunk reference is found.
func validateExplicitChunks(dataDir string, chunks []*domain.Chunks) ([]*domain.Chunks, error) {
	for _, chunk := range chunks {
		chunkRoot := filepath.Join(dataDir, chunk.Path)
		err := filepath.WalkDir(chunkRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink == 0 {
				return nil
			}

			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(path), linkTarget)
			}
			linkTarget = filepath.Clean(linkTarget)

			rel, err := filepath.Rel(dataDir, linkTarget)
			if err != nil || strings.HasPrefix(rel, "..") {
				return nil // target is outside data/ entirely — not a cross-chunk issue
			}

			topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]

			for _, other := range chunks {
				if other.Path == chunk.Path {
					continue
				}
				if topLevel == other.Path || strings.HasPrefix(rel, other.Path+string(filepath.Separator)) {
					symlinkRel, _ := filepath.Rel(dataDir, path)
					return fmt.Errorf(
						"symlink %q in chunk %q points to %q which belongs to chunk %q; "+
							"cross-chunk symlinks are not supported by ORAS",
						symlinkRel, chunk.Name, rel, other.Name,
					)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return chunks, nil
}
