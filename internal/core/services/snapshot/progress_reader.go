package services

import (
	"io"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProgressReader struct {
	*BasicTracker
	stream  io.ReadCloser
	current int64
}

func NewProgressReader(total int64, stream io.ReadCloser, basicTracker *BasicTracker) *ProgressReader {
	return &ProgressReader{
		BasicTracker: basicTracker,
		stream:       stream,
	}
}

func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.stream.Read(p)
	if n > 0 {
		pr.current += int64(n)
		pr.LogTrackProgress(pr.current)
	} else if err != nil {
		if err == io.EOF {
			pr.LogTrackProgress(pr.BasicTracker.total)
			return n, err
		}
		logger.Log().Error("Error reading progress", zap.Error(err))
		return n, err
	}
	return n, err
}

func (pr *ProgressReader) Close() error {
	if pr.stream != nil {
		err := pr.stream.Close()
		if err != nil {
			logger.Log().Error("Error closing stream", zap.Error(err))
			return err
		}
	}
	return nil
}
