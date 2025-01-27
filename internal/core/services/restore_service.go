package services

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
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

	// Define the source URL and destination directory
	err := rc.createTarGz(dir, target)
	if err != nil {
		return err
	}

	//TODO: upload
	if strings.HasPrefix(destination, "http") {
		err = rc.uploadFileUsingPresignedURL(destination, target)
		if err != nil {
			return err
		}
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

func (rc *RestoreService) uploadFileUsingPresignedURL(presignedURL, filePath string) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info to set the Content-Length header
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	fileSize := fileInfo.Size()

	// Create an HTTP request with the presigned URL
	req, err := http.NewRequest("PUT", presignedURL, file)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/octet-stream") // Adjust as needed
	req.Header.Set("Content-Length", fmt.Sprintf("%d", fileSize))

	// Execute the request
	client := &http.Client{}
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
