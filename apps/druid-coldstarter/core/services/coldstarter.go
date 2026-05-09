package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/core/ports"
	"github.com/highcard-dev/daemon/internal/core/domain"
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

func (s *ColdstarterService) RunWithRuntimeConfig(ctx context.Context, runtimeConfigPath string, statusFile string) error {
	data, err := os.ReadFile(runtimeConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read runtime config: %w", err)
	}
	var config domain.RuntimeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse runtime config: %w", err)
	}
	if len(config.Ports) == 0 {
		return fmt.Errorf("no ports found in runtime config")
	}

	logger.Log().Info("Coldstart runtime config loaded", zap.String("scroll", config.Scroll.ID), zap.Any("ports", config.Ports))
	runtimeRoot := filepath.Dir(runtimeConfigPath)
	if filepath.Base(runtimeRoot) == domain.RuntimeConfigDir {
		runtimeRoot = filepath.Dir(runtimeRoot)
	}
	portService := services.NewPortServiceWithScrollFile(&domain.File{Ports: config.Ports})
	coldStarter := services.NewColdStarter(portService, nil, runtimeRoot)

	finish := coldStarter.Start(ctx)
	select {
	case <-ctx.Done():
		coldStarter.Stop()
		return ctx.Err()
	case port := <-finish:
		coldStarter.Stop()
		if statusFile != "" && s.statusWriter != nil {
			if err := s.statusWriter.Write(runtimeRoot, statusFile, port); err != nil {
				return err
			}
		}
		logger.Log().Info("Coldstarter finished")
		return nil
	}
}

func (s *ColdstarterService) Run(ctx context.Context, scrollRoot string, statusFile string) error {
	scrollService, err := services.NewScrollService(scrollRoot)
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
			if err := s.statusWriter.Write(scrollRoot, statusFile, port); err != nil {
				return err
			}
		}
		logger.Log().Info("Coldstarter finished")
		return nil
	}
}
