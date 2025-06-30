package services

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type BasicTracker struct {
	total       int64
	lastPercent float64
}

func NewBasicTracker(total int64) *BasicTracker {
	return &BasicTracker{
		total:       total,
		lastPercent: 0,
	}
}

func (bt *BasicTracker) LogTrackProgress(current int64) {
	if bt.total > 0 {
		// percentage calculation
		currentPercent := (float64(current) * 100) / float64(bt.total)
		if currentPercent >= bt.lastPercent+0.1 {
			bt.lastPercent = currentPercent
			logger.Log().Info("Progress", zap.Int64("read", current), zap.Int64("total", bt.total), zap.String("percentage", fmt.Sprintf("%.1f%%", currentPercent)))
		}
	} else {
		logger.Log().Info("Progress", zap.Int64("read", current))
	}
}
func (bt *BasicTracker) GetPercent() float64 {
	if bt.total > 0 {
		return bt.lastPercent
	}
	return 0
}
