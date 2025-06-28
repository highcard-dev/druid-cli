package utils

import (
	"io"
	"os"
	"path/filepath"
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
