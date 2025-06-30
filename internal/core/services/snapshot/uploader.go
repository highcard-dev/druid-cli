package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

func Upload(rootPath, objectKey string, s3Destination *ports.S3Destination, compressionLevel int, progessTracker *BasicTracker) error {

	pipeReader, pipeWriter := io.Pipe()

	go func() {
		defer pipeWriter.Close()
		gzipWriter, err := gzip.NewWriterLevel(pipeWriter, compressionLevel)
		if err != nil {
			logger.Log().Error("Failed to create gzip writer", zap.Error(err))
			pipeWriter.CloseWithError(fmt.Errorf("failed to create gzip writer: %w", err))
			return
		}
		defer gzipWriter.Close()
		tarWriter := tar.NewWriter(gzipWriter)

		defer tarWriter.Close()

		progressFileWalk := NewProgressFileWalk(rootPath, progessTracker)
		progressFileWalk.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return archiveTarGzFile(path, info, err, rootPath, tarWriter)
		})
		println("Finished archiving files to tar.gz")
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
		Secure: !s3Destination.Insecure,
	})
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %v", err)
	}

	logger.Log().Info("Uploading snapshot to S3", zap.String("bucket", bucketName), zap.String("objectKey", objectKey), zap.String("endpoint", endpoint), zap.Bool("insecure", s3Destination.Insecure), zap.Int("compressionLevel", compressionLevel))

	contentType := "application/octet-stream"
	_, err = minioClient.PutObject(context.TODO(), bucketName, objectKey, pipeReader, -1, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func GetTotalFiles(dir string) (int64, error) {
	var totalFiles int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			totalFiles++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return totalFiles, nil
}
