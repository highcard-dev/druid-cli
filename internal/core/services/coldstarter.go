package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services/coldstarter/lua"
	"github.com/highcard-dev/daemon/internal/core/services/coldstarter/servers"
	"github.com/highcard-dev/daemon/internal/utils/logger"
)

type ColdStarter struct {
	portService ports.PortServiceInterface
	handler     map[string]uint
	finishCount uint
	dir         string
}

func NewColdStarter(
	portService ports.PortServiceInterface,
	dir string,
) *ColdStarter {
	return &ColdStarter{
		portService,
		make(map[string]uint),
		0,
		dir,
	}
}

func (c ColdStarter) StartLoop(ctx context.Context) error {
	return c.Start(ctx, false)
}
func (c ColdStarter) StartOnce(ctx context.Context) error {
	return c.Start(ctx, true)
}

func (c ColdStarter) Start(ctx context.Context, stopAfterFirst bool) error {
	augmentedPorts := c.portService.GetPorts()

	luactx, cancel := context.WithCancel(context.Background())

	doneChan := make(chan error)

	finishFunc := func() {
		c.finishCount++
		if stopAfterFirst {
			doneChan <- nil
		}
	}

	for _, port := range augmentedPorts {
		if port.Port.SleepHandler == nil {
			logger.Log().Warn(fmt.Sprintf("No sleep handler found for port %d", port.Port.Port))
			continue
		}

		path := fmt.Sprintf("%s/%s", c.dir, *port.Port.SleepHandler)

		go func(port *domain.AugmentedPort) {

			if port.Protocol == "udp" {
				logger.Log().Info(fmt.Sprintf("Starting UDP server on port %d", port.Port.Port))
				handler := lua.NewLuaHandler(path, c.dir)
				udpServer := servers.NewUDP(handler)
				err := udpServer.Start(luactx, port.Port.Port, finishFunc)
				if err != nil {
					doneChan <- err
				}
			} else if port.Protocol == "tcp" {
				logger.Log().Info(fmt.Sprintf("Starting TCP server on port %d", port.Port.Port))
				handler := lua.NewLuaHandler(path, c.dir)
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
