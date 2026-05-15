package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ColdstarterService struct{}

type envPortService struct {
	ports []*domain.AugmentedPort
}

func NewColdstarterService() *ColdstarterService {
	return &ColdstarterService{}
}

func (s *ColdstarterService) Run(ctx context.Context, root string) error {
	portService, err := portServiceFromEnv(root)
	if err != nil {
		return err
	}

	logger.Log().Info("Coldstart ports loaded", zap.Any("ports", portService.GetPorts()))

	coldStarter := services.NewColdStarter(portService, nil, root)

	finish := coldStarter.Start(ctx)
	select {
	case <-ctx.Done():
		coldStarter.Stop()
		return ctx.Err()
	case <-finish:
		coldStarter.Stop()
		logger.Log().Info("Coldstarter finished")
		return nil
	}
}

func (s *envPortService) GetPorts() []*domain.AugmentedPort {
	return s.ports
}

func portServiceFromEnv(root string) (*envPortService, error) {
	ports := []*domain.AugmentedPort{}
	vars := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if name, ok := strings.CutPrefix(key, "DRUID_COLDSTARTER_VAR_"); ok {
			if name != strings.ToUpper(name) {
				return nil, fmt.Errorf("%s must be uppercase", key)
			}
			vars[name] = value
			continue
		}
		if !strings.HasPrefix(key, "DRUID_PORT_") || !strings.HasSuffix(key, "_COLDSTARTER") {
			continue
		}
		if key != strings.ToUpper(key) {
			return nil, fmt.Errorf("%s must be uppercase", key)
		}
		handler := value
		suffix := strings.TrimSuffix(strings.TrimPrefix(key, "DRUID_PORT_"), "_COLDSTARTER")
		portValue := os.Getenv("DRUID_PORT_" + suffix)
		if portValue == "" {
			return nil, fmt.Errorf("DRUID_PORT_%s is required when %s is set", suffix, key)
		}
		if handler == "" {
			return nil, fmt.Errorf("%s must not be empty", key)
		}
		if handler != "generic" {
			path := filepath.Join(root, filepath.Clean(handler))
			if rel, err := filepath.Rel(root, path); err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") {
				return nil, fmt.Errorf("%s must be generic or a path below DRUID_ROOT", key)
			}
		}
		port, err := strconv.Atoi(portValue)
		if err != nil {
			return nil, fmt.Errorf("DRUID_PORT_%s must be a port number: %w", suffix, err)
		}
		protocol := strings.ToLower(os.Getenv("DRUID_PORT_" + suffix + "_PROTOCOL"))
		if protocol == "" {
			protocol = "tcp"
		}
		ports = append(ports, &domain.AugmentedPort{
			Port: domain.Port{
				Name:     strings.ToLower(suffix),
				Port:     port,
				Protocol: protocol,
			},
			ColdstarterHandler: handler,
			ColdstarterVars:    vars,
			InactiveSince:      time.Now(),
		})
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no coldstarter ports configured")
	}
	return &envPortService{ports: ports}, nil
}
