package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	lua "github.com/highcard-dev/daemon/internal/core/services/coldstarter/handler"
	"github.com/highcard-dev/daemon/internal/core/services/coldstarter/servers"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ColdStarter struct {
	handler     map[string]uint
	finishCount uint
	dir         string
	ports       []*domain.AugmentedPort
}

func NewColdStarter(
	dir string,
	ports []*domain.AugmentedPort,
) *ColdStarter {
	return &ColdStarter{
		make(map[string]uint),
		0,
		dir,
		ports,
	}
}

func (c ColdStarter) StartLoop(ctx context.Context) error {
	return c.Start(ctx, false)
}
func (c ColdStarter) StartOnce(ctx context.Context) error {
	return c.Start(ctx, true)
}

func (c ColdStarter) Start(ctx context.Context, stopAfterFirst bool) error {
	augmentedPorts := c.ports

	luactx, cancel := context.WithCancel(context.Background())

	doneChan := make(chan error)

	finishFunc := func() {
		c.finishCount++
		if stopAfterFirst {
			doneChan <- nil
		}
	}

	augmentedPortMap := make(map[string]int, len(augmentedPorts))
	for _, p := range augmentedPorts {
		augmentedPortMap[p.Name] = p.Port.Port
	}

	for _, port := range augmentedPorts {
		var sleepHandler string
		if port.SleepHandler == nil {
			logger.Log().Warn(fmt.Sprintf("No sleep handler found for port %d, using generic", port.Port.Port))
			sleepHandler = "generic"
		} else {
			sleepHandler = *port.SleepHandler
		}

		path := fmt.Sprintf("%s/%s", c.dir, sleepHandler)

		go func(port *domain.AugmentedPort) {

			var handler ports.ColdStarterInterface

			if sleepHandler == "generic" {
				handler = lua.NewGenericReturnHandler()
			} else {
				vars := make(map[string]string, len(port.Vars))
				for _, v := range port.Vars {
					vars[v.Name] = v.Value
				}

				handler = lua.NewLuaHandler(path, c.dir, vars, augmentedPortMap)
			}

			if port.Protocol == "udp" {
				logger.Log().Info(fmt.Sprintf("Starting UDP server on port %d", port.Port.Port), zap.String("sleep_handler", sleepHandler), zap.String("port_name", port.Name))
				udpServer := servers.NewUDP(handler)
				err := udpServer.Start(luactx, port.Port.Port, finishFunc)
				if err != nil {
					doneChan <- err
				}
			} else if port.Protocol == "tcp" {
				logger.Log().Info(fmt.Sprintf("Starting TCP server on port %d", port.Port.Port))
				tcpServer := servers.NewTCP(handler)
				err := tcpServer.Start(luactx, port.Port.Port, finishFunc)
				if err != nil {
					doneChan <- err
				}
			} else {
				logger.Log().Warn(fmt.Sprintf("Unknown protocol %s for coldstarter", port.Protocol))
				return
			}
			logger.Log().Info(fmt.Sprintf("Server on port %d received finish signal", port.Port.Port))
		}(port)
	}

	select {
	case err := <-doneChan:
		cancel()
		return err
	case <-ctx.Done():
		cancel()
		return nil
	}
}

func (c ColdStarter) FinishCount() uint {
	return c.finishCount
}
