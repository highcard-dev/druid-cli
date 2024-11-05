package services

import (
	"context"
	"fmt"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	lua "github.com/highcard-dev/daemon/internal/core/services/coldstarter/handler"
	"github.com/highcard-dev/daemon/internal/core/services/coldstarter/servers"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ColdStarter struct {
	handler      map[string]uint
	finishCount  uint
	dir          string
	finishTime   *time.Time
	portsService ports.PortServiceInterface
	serveDone    chan error
	finishChan   chan *domain.AugmentedPort
	chandlers    []ports.ColdStarterHandlerInterface
}

// NewColdStarter initializes the ColdStarter struct with proper channel initialization and no initial finishTime.
func NewColdStarter(
	dir string,
	portsService ports.PortServiceInterface,
) *ColdStarter {
	return &ColdStarter{
		handler:      make(map[string]uint),
		finishCount:  0,
		dir:          dir,
		finishTime:   nil,
		portsService: portsService,
		serveDone:    make(chan error),
		finishChan:   make(chan *domain.AugmentedPort),
		chandlers:    nil,
	}
}

// Start initializes the finishChan channel and begins serving in a separate goroutine.
func (c *ColdStarter) Start(ctx context.Context) chan *domain.AugmentedPort {
	// Ensure finishChan is properly initialized.
	c.finishChan = make(chan *domain.AugmentedPort)

	go c.Serve(ctx)

	return c.finishChan
}

// FinishCount returns the current count of finished ports.
func (c *ColdStarter) FinishCount() uint {
	return c.finishCount
}

// Serve starts the servers for each port and listens for context cancellation or errors.
func (c *ColdStarter) Serve(ctx context.Context) error {
	augmentedPorts := c.portsService.GetPorts()

	augmentedPortMap := make(map[string]int, len(augmentedPorts))
	for _, p := range augmentedPorts {
		augmentedPortMap[p.Name] = p.Port.Port
	}

	luactx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancel is called to release resources.

	// Initialize the handler map with a length of augmentedPorts
	c.handler = make(map[string]uint, len(augmentedPorts))

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
			var handler ports.ColdStarterHandlerInterface

			if sleepHandler == "generic" {
				handler = lua.NewGenericReturnHandler()
			} else {
				vars := make(map[string]string, len(port.Vars))
				for _, v := range port.Vars {
					vars[v.Name] = v.Value
				}
				handler = lua.NewLuaHandler(path, c.dir, vars, augmentedPortMap)
			}

			c.chandlers = append(c.chandlers, handler)

			// Use the Finish method to handle sending to finishChan.
			finishFunc := func() {
				c.Finish(port)
			}

			if port.Protocol == "udp" {
				logger.Log().Info(fmt.Sprintf("Starting UDP server on port %d", port.Port.Port), zap.String("sleep_handler", sleepHandler), zap.String("port_name", port.Name))
				udpServer := servers.NewUDP(handler)
				err := udpServer.Start(luactx, port.Port.Port, finishFunc)
				if err != nil {
					c.serveDone <- err
				}
			} else if port.Protocol == "tcp" {
				logger.Log().Info(fmt.Sprintf("Starting TCP server on port %d", port.Port.Port))
				tcpServer := servers.NewTCP(handler)
				err := tcpServer.Start(luactx, port.Port.Port, finishFunc)
				if err != nil {
					c.serveDone <- err
				}
			} else {
				logger.Log().Warn(fmt.Sprintf("Unknown protocol %s for coldstarter", port.Protocol))
				return
			}
		}(port)
	}

	select {
	case err := <-c.serveDone:
		cancel()
		return err
	case <-ctx.Done():
		cancel()
		return nil
	}
}

// Stop sends a nil error to the serveDone channel to gracefully stop the Serve function.
func (c *ColdStarter) Stop() {
	c.serveDone <- nil
}

// Finish increments the finishCount, logs, and sends the port to the finishChan channel.
// This method uses a pointer receiver to ensure it modifies the original struct.
func (c *ColdStarter) Finish(port *domain.AugmentedPort) {
	if c.finishTime == nil {
		now := time.Now()
		c.finishTime = &now

		for _, handler := range c.chandlers {
			handler.SetFinishedAt(c.finishTime)
		}
	}
	if port == nil {
		logger.Log().Info("Received finish signal without port")
	} else {
		logger.Log().Info(fmt.Sprintf("Server on port %d received finish signal", port.Port.Port))
	}
	c.finishChan <- port
	c.finishCount++
}
