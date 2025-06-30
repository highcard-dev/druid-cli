package services

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// extractTarGzFromReader extracts a gzipped tar archive from a reader
func extractTarGzFromReader(dir string, reader io.Reader) error {
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	return extractTarFromReader(dir, gzReader)
}

// extractTarFromReader extracts a tar archive from an io.Reader
func extractTarFromReader(dir string, reader io.Reader) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		if err := extractTarFile(dir, tarReader, header); err != nil {
			return err
		}
	}

	return nil
}

// extractTarFile extracts a single file from tar archive
func extractTarFile(dir string, tarReader *tar.Reader, header *tar.Header) error {
	path := filepath.Join(dir, header.Name)

	switch header.Typeflag {
	case tar.TypeDir:
		//check if the directory already exists
		if _, err := os.Stat(path); err == nil {
			// Directory exists
			return nil
		}

		if err := os.Mkdir(path, 0755); err != nil {
			return fmt.Errorf("ExtractTarGz: Mkdir() failed: %w", err)
		}
	case tar.TypeReg:
		outFile, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("ExtractTarGz: Create() failed: %w", err)
		}
		if _, err := io.Copy(outFile, tarReader); err != nil {
			return fmt.Errorf("ExtractTarGz: Copy() failed: %w", err)
		}
		outFile.Close()
	case tar.TypeSymlink:
		// Create a symlink
		if err := os.Symlink(header.Linkname, path); err != nil {
			return fmt.Errorf("ExtractTarGz: Symlink() failed: %w", err)
		}
	case tar.TypeLink:
		// Create a hard link
		if err := os.Link(header.Linkname, path); err != nil {
			return fmt.Errorf("ExtractTarGz: Link() failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported tar header type: %c", header.Typeflag)
	}

	return nil
}

// extractTarFile extracts a single file from tar archive
func archiveTarGzFile(path string, info os.FileInfo, err error, dir string, tarWriter *tar.Writer) error {
	// Walk through the source directory
	if err != nil {
		return err
	}

	linkName := ""
	if info.Mode()&os.ModeSymlink == os.ModeSymlink {
		linkName, err = os.Readlink(path)
		if err != nil {
			return err
		}
	}

	hdr, err := tar.FileInfoHeader(info, linkName)
	if err != nil {
		return err
	}

	hdr.Name, _ = filepath.Rel(dir, path)

	if err := tarWriter.WriteHeader(hdr); err != nil {
		return err
	}

	if info.Mode().IsRegular() {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tarWriter, file)
		return err
	}

	return nil
}
