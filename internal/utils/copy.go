package utils

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyPath copies files, directories and symlinks without dereferencing links.
// Ordinary access bits are retained regardless of umask. preserveOwner additionally
// retains numeric UID/GID, failing if this process cannot set them. Callers must
// supply a trusted staging destination, not a live tree with symlink parents.
func CopyPath(src, dst string, preserveOwner bool) (err error) {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil && preserveOwner {
			var header *tar.Header
			header, err = tar.FileInfoHeader(info, "")
			if err == nil {
				err = SetPathOwner(dst, header.Uid, header.Gid)
			}
		}
	}()
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		// Keep owner access while populating; finalize read-only directories last.
		if err := os.MkdirAll(dst, 0700); err != nil {
			return err
		}
		if err := os.Chmod(dst, info.Mode().Perm()|0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := CopyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), preserveOwner); err != nil {
				return err
			}
		}
		return os.Chmod(dst, info.Mode().Perm())
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported copy source type: %s", info.Mode().Type())
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(info.Mode().Perm())
}

// SetPathOwner changes numeric ownership only when needed, never following a
// symlink. Source trees and external symlink targets must not be mutated.
func SetPathOwner(path string, uid, gid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	if uid == header.Uid && gid == header.Gid {
		return nil
	}
	if err := os.Lchown(path, uid, gid); err != nil {
		return fmt.Errorf("set materialized path owner: %w", err)
	}
	return nil
}
