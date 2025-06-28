package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/hashicorp/go-getter"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

type SnapshotService struct {
	currentProgressTracker ports.ProgressTracker
	currentMode            ports.SnapshotMode
}

func NewSnapshotService() *SnapshotService {
	return &SnapshotService{
		currentMode: ports.SnapshotModeNoop,
	}
}

func (rc *SnapshotService) setActivity(mode ports.SnapshotMode, progressTracker ports.ProgressTracker) {
	rc.currentMode = mode
	rc.currentProgressTracker = progressTracker
}

func (rc *SnapshotService) GetCurrentProgressTracker() *ports.ProgressTracker {
	if rc.currentMode == ports.SnapshotModeNoop {
		return nil
	}
	return &rc.currentProgressTracker
}

func (rc *SnapshotService) countFilesRec(dir string) (int64, error) {
	var fileCount int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			fileCount++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return fileCount, nil
}

func (rc *SnapshotService) Snapshot(dir string, destination string, options ports.SnapshotOptions) error {

	totalFiles := int64(0)
	totalFiles, _ = rc.countFilesRec(dir)
	progessTracker := NewGeneralProgressTracker(totalFiles)

	rc.setActivity(ports.SnapshotModeSnapshot, progessTracker)
	defer rc.setActivity(ports.SnapshotModeNoop, progessTracker)
	//check if rootPath exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("source path does not exist: %s", dir)
	}

	return rc.uploadS3(dir, destination, options.S3Destination, options.CompressionLevel, progessTracker)
}

func (rc *SnapshotService) RestoreSnapshot(dir string, source string, options ports.RestoreSnapshotOptions) error {

	progressReader := &ProgressTracker{}

	dest := path.Join(dir, ".snap_dl")

	os.RemoveAll(dest)
	defer os.RemoveAll(dest)

	// Create a new client
	client := &getter.Client{
		Src:              source, // Source URL
		Dst:              dest,
		Mode:             getter.ClientModeDir,
		ProgressListener: progressReader,
	}
	_, err := getter.Detect(client.Src, client.Pwd, client.Detectors)
	if err != nil {
		return fmt.Errorf("failed to validate source: %w", err)
	}

	if _, err := os.Stat(dir); err == nil {
		logger.Log().Info("Removing folder to make space for backup", zap.String("dir", dir))
		err := utils.RemoveContents(dir)
		if err != nil {
			return err
		}
	}

	rc.setActivity(ports.SnapshotModeRestore, progressReader)
	defer rc.setActivity(ports.SnapshotModeNoop, nil)

	logger.Log().Info("Restoring backup", zap.String("source", source), zap.String("destination", dir))

	// Download the file
	err = client.Get()
	if err != nil {
		os.RemoveAll(dest)
		logger.Log().Error("Error occured while getting backup", zap.Error(err))
		return err
	}

	// Move the downloaded file to the destination
	err = utils.MoveContents(dest, dir)
	if err != nil {
		return err
	}

	logger.Log().Info("Backup restored", zap.String("source", source), zap.String("destination", dir))
	return nil
}

func (rc *SnapshotService) uploadS3(rootPath, objectKey string, s3Destination *ports.S3Destination, compressionLevel int, progessTracker *GeneralProgressTracker) error {

	pipeReader, pipeWriter := io.Pipe()

	go func() {
		defer pipeWriter.Close()

		// Create a gzip writer
		gzipWriter, err := gzip.NewWriterLevel(pipeWriter, compressionLevel)
		if err != nil {
			return
		}
		defer gzipWriter.Close()

		// Create a tar writer
		tarWriter := tar.NewWriter(gzipWriter)
		defer tarWriter.Close()

		// Walk through the source directory
		filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
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
				progessTracker.TrackProgress()
				return err
			}

			return nil
		})
	}()

	endpoint := s3Destination.Endpoint
	region := s3Destination.Region
	if region == "" {
		region = "us-east-1"
	}
	accessKey := s3Destination.AccessKey
	secretKey := s3Destination.SecretKey
	bucketName := s3Destination.Bucket

	// Load AWS config with custom S3-compatible settings
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %v", err)
	}

	contentType := "application/octet-stream"
	_, err = minioClient.PutObject(context.TODO(), bucketName, objectKey, pipeReader, -1, minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Todo: refactor this to do streaming upload
func (rc *SnapshotService) uploadFileUsingPresignedURL(presignedURL, filePath string) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get the file size
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	fileSize := fileInfo.Size()

	// Wrap the file reader in the ProgressReader with an update frequency of 1 second
	progressReader := &ProgressTracker{
		reader:   file,
		fileSize: fileSize,
	}

	// Create an HTTP request with the presigned URL
	req, err := http.NewRequest("PUT", presignedURL, progressReader)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/octet-stream") // Adjust as needed
	req.Header.Set("Content-Length", fmt.Sprintf("%d", fileSize))

	transport := &http.Transport{
		ForceAttemptHTTP2: false, // disable http2, to prevent REFUSED_STREAM errors
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	// Use a HTTP client with automatic retries configured, if possible
	client := &http.Client{
		Transport: transport,
	}

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

func (rc *SnapshotService) GetProgressTracker() *ports.ProgressTracker {
	if rc.currentMode == ports.SnapshotModeNoop {
		return nil
	}
	return &rc.currentProgressTracker
}

func (rc *SnapshotService) GetCurrentMode() ports.SnapshotMode {
	return rc.currentMode
}
