package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ColdstarterService struct {
	statusWriter ports.StatusWriter
}

func NewColdstarterService(statusWriter ports.StatusWriter) *ColdstarterService {
	return &ColdstarterService{statusWriter: statusWriter}
}

func (s *ColdstarterService) Run(ctx context.Context, root string, statusFile string) error {
	scrollService, err := services.NewScrollService(root)
	if err != nil {
		return fmt.Errorf("failed to load scroll: %w", err)
	}

	currentScroll := scrollService.GetCurrent()
	if len(currentScroll.Ports) == 0 {
		return fmt.Errorf("no ports found in scroll")
	}

	logger.Log().Info("Coldstart scroll loaded", zap.String("name", currentScroll.Name), zap.Any("version", currentScroll.Version), zap.Any("ports", currentScroll.Ports))

	portService := services.NewPortServiceWithScrollFile(&currentScroll.File)
	coldStarter := services.NewColdStarter(portService, nil, scrollService.GetDir())

	finish := coldStarter.Start(ctx)
	select {
	case <-ctx.Done():
		coldStarter.Stop()
		return ctx.Err()
	case port := <-finish:
		coldStarter.Stop()
		if statusFile != "" && s.statusWriter != nil {
			if err := s.statusWriter.Write(root, statusFile, port); err != nil {
				return err
			}
		}
		logger.Log().Info("Coldstarter finished")
		return nil
	}
}
