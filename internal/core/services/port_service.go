package services

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type PortService struct {
	ports []*domain.AugmentedPort
}

func NewPortServiceWithScrollFile(file *domain.File) *PortService {
	p := &PortService{}
	p.SyncPortEnv(file)
	return p
}

func (p *PortService) SyncPortEnv(file *domain.File) []*domain.AugmentedPort {
	var augmentedPorts []*domain.AugmentedPort
	for _, port := range file.Ports {
		portEnvName := fmt.Sprintf("DRUID_PORT_%s", strings.ToUpper(port.Name))
		envPort := os.Getenv(portEnvName)
		if envPort != "" && port.Port == 0 {
			portInt, err := strconv.Atoi(envPort)
			if err == nil {
				port.Port = portInt
			}
		}
		if port.Port == 0 {
			logger.Log().Warn("Could not find port number for port", zap.String("port", port.Name))
			continue
		}
		augmentedPorts = append(augmentedPorts, &domain.AugmentedPort{
			Port:          port,
			InactiveSince: time.Now(),
		})
		os.Setenv(portEnvName, strconv.Itoa(port.Port))
	}
	p.ports = augmentedPorts
	return p.ports
}

func (p *PortService) GetPorts() []*domain.AugmentedPort {
	return p.ports
}
