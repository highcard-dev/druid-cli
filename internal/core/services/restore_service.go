package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-getter"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type RestoreService struct {
}

func NewRestoreService() *RestoreService {
	return &RestoreService{}
}

func (rc *RestoreService) Snapshot(dir string, destination string, options ports.SnapshotOptions) error {

	var target string
	if options.TempDir == "" {
		target = filepath.Join(dir, "snapshot.tgz")
	} else {
		target = filepath.Join(options.TempDir, "snapshot.tgz")
	}

	logger.Log().Info("Creating snapshot", zap.String("source", dir), zap.String("destination", target))
	// Define the source URL and destination directory
	err := rc.createTarGz(dir, target)
	if err != nil {
		return err
	}
	logger.Log().Info("Snapshot created", zap.String("source", dir), zap.String("destination", target))

	//TODO: upload
	if strings.HasPrefix(destination, "http") {
		logger.Log().Info("Uploading snapshot", zap.String("source", target), zap.String("destination", destination))
		err = rc.uploadFileUsingPresignedURL(destination, target)
		if err != nil {
			logger.Log().Error("Error occured while uploading snapshot", zap.Error(err))
			return err
		}
		logger.Log().Info("Snapshot uploaded", zap.String("source", target), zap.String("destination", destination))
	} else {
		return errors.New("destination must be a presigned S3 URL")
	}

	return os.Remove(target)
}

func (rc *RestoreService) RestoreSnapshot(dir string, source string, options ports.RestoreSnapshotOptions) error {

	temDir := options.TempDir
	if temDir == "" {
		temDir = dir + "-bck"
	}

	//move dir if possible
	if _, err := os.Stat(dir); err == nil {
		if options.Safe {
			logger.Log().Info("Moving folder to make space for backup", zap.String("dir", dir), zap.String("backup_dir", dir+"-bck"))
			err := os.Rename(dir, temDir)
			if err != nil {
				return nil
			}
		} else {
			err := os.Remove(dir)
			if err != nil {
				return nil
			}
		}
	}

	// Create a new client
	client := &getter.Client{
		Src:  source, // Source URL
		Dst:  dir,    // Destination path
		Mode: getter.ClientModeDir,
	}
	logger.Log().Info("Restoring backup", zap.String("source", source), zap.String("destination", dir))

	// Download the file
	err := client.Get()

	logger.Log().Info("Backup restored", zap.String("source", source), zap.String("destination", dir))

	if err != nil {
		logger.Log().Error("Error occured while getting backup", zap.Error(err))
		if options.Safe {
			logger.Log().Warn("Restoring old state, as error occured while getting backup", zap.Error(err))
			errRename := os.Rename(temDir, dir)
			if errRename != nil {
				return errRename
			}
		}
		return err
	}
	return os.RemoveAll(temDir)
}

func (rc *RestoreService) createTarGz(rootPath, target string) error {
	// Create the target .tgz file
	tgzFile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer tgzFile.Close()

	// Create a gzip writer
	gzipWriter := gzip.NewWriter(tgzFile)
	defer gzipWriter.Close()

	// Create a tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Walk through the source directory
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the target file
		if absTarget, err := filepath.Abs(target); err != nil {
			return err
		} else if absFile, err := filepath.Abs(path); err != nil {
			return err
		} else if absFile == absTarget {
			return nil
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

		hdr.Name, _ = filepath.Rel(rootPath, path)

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
	})
}

type ProgressReader struct {
	reader      io.Reader
	read        int64
	fileSize    int64
	lastPercent float64
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)

	// Calculate current percentage of upload progress
	currentPercent := (float64(pr.read) * 100) / float64(pr.fileSize)

	// Update progress if we've moved at least 0.1% or it's been more than the update frequency since the last update
	if currentPercent > pr.lastPercent+0.1 {
		logger.Log().Info("Upload progress", zap.Float64("percentage", currentPercent))
		pr.lastPercent = currentPercent
	}

	// If the upload is finished
	if pr.read == pr.fileSize {
		logger.Log().Info("Upload complete")
	}

	return n, err
}

func (rc *RestoreService) uploadFileUsingPresignedURL(presignedURL, filePath string) error {
	// Read the file into memory
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Convert data to io.Reader/io.ReadCloser
	fileReader := bytes.NewReader(data)

	// Wrap the reader in the ProgressReader
	progressReader := &ProgressReader{
		reader:   fileReader,
		fileSize: int64(len(data)),
	}

	// Create an HTTP request with the presigned URL
	req, err := http.NewRequest("PUT", presignedURL, progressReader)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Define GetBody to allow retries by providing a new reader each time
	req.GetBody = func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewReader(data)), nil
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/octet-stream") // Adjust as needed
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Use a HTTP client with automatic retries configured, if possible
	client := &http.Client{}

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("upload failed with status code %d", resp.StatusCode)
	}

	fmt.Println("File uploaded successfully")
	return nil
}
