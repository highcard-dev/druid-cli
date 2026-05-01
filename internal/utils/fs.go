package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
// Explicit mode (explicitChunks provided): expands any globbed or nested chunk
// declarations into concrete non-overlapping chunks, then validates that no
// symlink inside a chunk's directory tree points into a different chunk's tree
// (cross-chunk symlinks cannot be extracted by ORAS). Returns an error if
// detected.
func AutoChunkDataDir(dataDir string, explicitChunks []*domain.Chunks) ([]*domain.Chunks, error) {
	if len(explicitChunks) > 0 {
		chunks, err := expandExplicitChunks(dataDir, explicitChunks)
		if err != nil {
			return nil, err
		}
		if err := validateNoOverlappingChunks(chunks); err != nil {
			return nil, err
		}
		return validateExplicitChunks(dataDir, chunks)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	hasSymlink := false
	_ = filepath.WalkDir(dataDir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			hasSymlink = true
			return filepath.SkipAll
		}
		return nil
	})

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

func expandExplicitChunks(dataDir string, chunks []*domain.Chunks) ([]*domain.Chunks, error) {
	expanded := make([]*domain.Chunks, 0, len(chunks))
	for _, chunk := range chunks {
		chunks, err := expandChunkNode(dataDir, "", chunk)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, chunks...)
	}
	return expanded, nil
}

func expandChunkNode(dataDir string, parentPath string, chunk *domain.Chunks) ([]*domain.Chunks, error) {
	if chunk == nil {
		return nil, nil
	}
	if chunk.Path == "" {
		return nil, fmt.Errorf("chunk %q has empty path", chunk.Name)
	}

	paths, err := expandChunkPaths(dataDir, parentPath, chunk.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to expand chunk %q path %q: %w", chunk.Name, chunk.Path, err)
	}

	expanded := make([]*domain.Chunks, 0, len(paths))
	for _, chunkPath := range paths {
		if len(chunk.Chunks) == 0 {
			expanded = append(expanded, &domain.Chunks{
				Name: chunkName(chunk.Name, chunkPath, len(paths) > 1),
				Path: chunkPath,
			})
			continue
		}

		childChunks := make([]*domain.Chunks, 0, len(chunk.Chunks))
		for _, child := range chunk.Chunks {
			children, err := expandChunkNode(dataDir, chunkPath, child)
			if err != nil {
				return nil, err
			}
			for _, childChunk := range children {
				if !isSamePath(childChunk.Path, chunkPath) && !isPathWithin(childChunk.Path, chunkPath) {
					return nil, fmt.Errorf("child chunk %q path %q is outside parent chunk %q path %q", childChunk.Name, childChunk.Path, chunk.Name, chunkPath)
				}
			}
			childChunks = append(childChunks, children...)
		}

		remainderChunks, err := expandParentRemainder(dataDir, chunk, chunkPath, childChunks)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, remainderChunks...)
		expanded = append(expanded, childChunks...)
	}

	return expanded, nil
}

func expandChunkPaths(dataDir string, parentPath string, chunkPath string) ([]string, error) {
	resolvedPath, err := resolveChunkPath(parentPath, chunkPath)
	if err != nil {
		return nil, err
	}

	if !hasGlobMeta(resolvedPath) {
		return []string{resolvedPath}, nil
	}

	matches, err := filepath.Glob(filepath.Join(dataDir, filepath.FromSlash(resolvedPath)))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("glob matched no files")
	}
	sort.Strings(matches)

	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(dataDir, match)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if pathEscapesRoot(rel) {
			return nil, fmt.Errorf("glob match %q escapes data directory", rel)
		}
		paths = append(paths, rel)
	}
	return paths, nil
}

func resolveChunkPath(parentPath string, chunkPath string) (string, error) {
	chunkPath = filepath.ToSlash(strings.TrimSpace(chunkPath))
	if strings.HasPrefix(chunkPath, "/") {
		chunkPath = strings.TrimLeft(chunkPath, "/")
	} else if parentPath != "" {
		chunkPath = filepath.ToSlash(filepath.Join(filepath.FromSlash(parentPath), filepath.FromSlash(chunkPath)))
	}

	chunkPath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(chunkPath)))
	if chunkPath == "." || pathEscapesRoot(chunkPath) {
		return "", fmt.Errorf("path %q escapes data directory", chunkPath)
	}
	return chunkPath, nil
}

func expandParentRemainder(dataDir string, chunk *domain.Chunks, parentPath string, childChunks []*domain.Chunks) ([]*domain.Chunks, error) {
	parentFullPath := filepath.Join(dataDir, filepath.FromSlash(parentPath))
	entries, err := os.ReadDir(parentFullPath)
	if err != nil {
		return nil, err
	}

	excludedEntries := map[string]struct{}{}
	for _, child := range childChunks {
		childRel, err := filepath.Rel(filepath.FromSlash(parentPath), filepath.FromSlash(child.Path))
		if err != nil {
			return nil, err
		}
		childRel = filepath.ToSlash(filepath.Clean(childRel))
		if childRel == "." || pathEscapesRoot(childRel) {
			return nil, fmt.Errorf("child chunk %q path %q is outside parent chunk %q path %q", child.Name, child.Path, chunk.Name, parentPath)
		}
		entryName := strings.SplitN(childRel, "/", 2)[0]
		excludedEntries[entryName] = struct{}{}
	}

	remainderChunks := make([]*domain.Chunks, 0, len(entries))
	for _, entry := range entries {
		if _, excluded := excludedEntries[entry.Name()]; excluded {
			continue
		}
		remainderPath := filepath.ToSlash(filepath.Join(filepath.FromSlash(parentPath), entry.Name()))
		remainderChunks = append(remainderChunks, &domain.Chunks{
			Name: chunkName(chunk.Name, remainderPath, true),
			Path: remainderPath,
		})
	}
	return remainderChunks, nil
}

func validateNoOverlappingChunks(chunks []*domain.Chunks) error {
	for i := 0; i < len(chunks); i++ {
		for j := i + 1; j < len(chunks); j++ {
			left := filepath.ToSlash(filepath.Clean(filepath.FromSlash(chunks[i].Path)))
			right := filepath.ToSlash(filepath.Clean(filepath.FromSlash(chunks[j].Path)))
			if isSamePath(left, right) || isPathWithin(left, right) || isPathWithin(right, left) {
				return fmt.Errorf("chunk %q path %q overlaps chunk %q path %q", chunks[i].Name, left, chunks[j].Name, right)
			}
		}
	}
	return nil
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func chunkName(name string, path string, withSuffix bool) string {
	if name == "" {
		name = "chunk"
	}
	if !withSuffix {
		return name
	}
	return name + "-" + sanitizeChunkName(filepath.Base(filepath.FromSlash(path)))
}

func sanitizeChunkName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	lastWasDash := false
	for _, r := range name {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastWasDash = false
			continue
		}
		if !lastWasDash {
			b.WriteByte('-')
			lastWasDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func isSamePath(left string, right string) bool {
	return filepath.Clean(filepath.FromSlash(left)) == filepath.Clean(filepath.FromSlash(right))
}

func isPathWithin(path string, parent string) bool {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	parent = filepath.ToSlash(filepath.Clean(filepath.FromSlash(parent)))
	return strings.HasPrefix(path, parent+"/")
}

func pathEscapesRoot(path string) bool {
	return path == ".." || strings.HasPrefix(path, "../")
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
				if topLevel == other.Path || rel == other.Path || strings.HasPrefix(rel, other.Path+string(filepath.Separator)) {
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
