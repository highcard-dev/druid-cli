package services

import (
	"fmt"
	"io"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProgressTracker struct {
	reader      io.ReadCloser
	read        int64
	fileSize    int64
	lastPercent float64
}

func (pr *ProgressTracker) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)

	// Calculate current percentage of upload progress
	currentPercent := (float64(pr.read) * 100) / float64(pr.fileSize)

	// Update progress if we've moved at least 0.1% or it's been more than the update frequency since the last update
	if currentPercent > pr.lastPercent+0.1 {
		logger.Log().Info("Snapshot operation progress", zap.String("percentage", fmt.Sprintf("%.1f%%", currentPercent)), zap.String("read", fmt.Sprintf("%d/%d", pr.read, pr.fileSize)))
		pr.lastPercent = currentPercent
	}

	// If the upload is finished
	if pr.read == pr.fileSize {
		logger.Log().Info("Snapshot operation complete")
	}

	return n, err
}

func (pr *ProgressTracker) Close() error {
	pr.lastPercent = 100
	return pr.reader.Close()
}

func (pt *ProgressTracker) TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) io.ReadCloser {
	pt.reader = stream
	pt.fileSize = totalSize
	logger.Log().Info("Snapshot operation started", zap.String("source", src), zap.String("size", fmt.Sprintf("%d bytes", totalSize)))
	return pt
}

func (pt *ProgressTracker) GetPercent() float64 {
	return pt.lastPercent
}
