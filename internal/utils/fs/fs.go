package fs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"gopkg.in/yaml.v3"
)

func RecreateDir(dir string) error {
	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}
	err = os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func TarDirectory(srcPath string, destinationPath string) error {
	var buf bytes.Buffer
	zr := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zr)
	filepath.Walk(srcPath, func(file string, fi os.FileInfo, err error) error {
		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}
		fileName := strings.TrimPrefix(strings.TrimPrefix(file, srcPath), strings.TrimPrefix(srcPath, "./"))
		if fileName == "" {
			header.Name = "./"
		} else {
			header.Name = "." + filepath.ToSlash(fileName)
		}

		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// if not a dir, write file content
		if !fi.IsDir() {
			data, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}
		return nil
	})

	if err := tw.Close(); err != nil {
		return err
	}
	if err := zr.Close(); err != nil {
		return err
	}
	fileToWrite, err := os.OpenFile(destinationPath, os.O_RDWR|os.O_CREATE, os.FileMode(777))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fileToWrite, &buf); err != nil {
		return err
	}
	return nil
}

func CreateYamlFile[T any](data T, destinationPath string) error {
	yamlData, err := yaml.Marshal(&data)

	if err != nil {
		return err
	}
	err = os.WriteFile(destinationPath, yamlData, 0644)
	if err != nil {
		return err
	}
	return nil
}

func OpenScrollYaml(path string) (*domain.File, error) {
	jsonFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)
	var scroll domain.File
	err = yaml.Unmarshal(byteValue, &scroll)
	if err != nil {
		return nil, err
	}
	return &scroll, nil
}

func UntarFile(dst string, file []byte) error {
	gzr, err := gzip.NewReader(bytes.NewReader(file))
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(dst, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}
