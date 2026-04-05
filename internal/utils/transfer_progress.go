package utils

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

// countingReader wraps r and adds every successful Read size to transferred.
type countingReader struct {
	inner       io.Reader
	transferred *atomic.Int64
}

// NewCountingReader returns an io.Reader that counts bytes read into transferred.
func NewCountingReader(r io.Reader, transferred *atomic.Int64) io.Reader {
	return &countingReader{inner: r, transferred: transferred}
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		r.transferred.Add(int64(n))
	}
	return n, err
}

// StartTransferProgressTicker logs periodic progress and a final "done" line
// (transferred, total, layers, average speed, elapsed on done).
// Call the returned function when the transfer finishes.
func StartTransferProgressTicker(direction string, bytesTransferred, totalBytes, layersDone, totalLayers *atomic.Int64) func() {
	ticker := time.NewTicker(60 * time.Second)
	done := make(chan struct{})
	start := time.Now()

	report := func(event string, withElapsed bool) {
		n := bytesTransferred.Load()
		sec := time.Since(start).Seconds()
		speed := ""
		if sec > 0 {
			speed = HumanizeBytes(int64(float64(n)/sec)) + "/s"
		}
		fields := []zap.Field{
			zap.String("transferred", HumanizeBytes(n)),
			zap.String("total", HumanizeBytes(totalBytes.Load())),
			zap.String("layers", fmt.Sprintf("%d/%d", layersDone.Load(), totalLayers.Load())),
			zap.String("speed", speed),
		}
		if withElapsed {
			fields = append(fields, zap.Duration("elapsed", time.Since(start)))
		}
		logger.Log().Info(direction+" "+event, fields...)
	}

	go func() {
		for {
			select {
			case <-ticker.C:
				report("progress", false)
			case <-done:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		report("done", true)
	}
}
