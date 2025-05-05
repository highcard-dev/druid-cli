package services

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type GeneralProgressTracker struct {
	total int64
	read  int64
}

func NewGeneralProgressTracker(total int64) *GeneralProgressTracker {
	return &GeneralProgressTracker{
		total: total,
		read:  0,
	}
}

func (pt *GeneralProgressTracker) GetPercent() float64 {
	return (float64(pt.read) / float64(pt.total)) * 100
}
func (pt *GeneralProgressTracker) TrackProgress() {
	pt.read++
	logger.Log().Info("Progress", zap.Int64("total", pt.total), zap.Int64("read", pt.read), zap.String("percentage", fmt.Sprintf("%.1f%%", pt.GetPercent())))
}
