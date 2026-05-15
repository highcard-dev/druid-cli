package services

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	lua "github.com/highcard-dev/daemon/internal/core/services/coldstarter/handler"
	"github.com/highcard-dev/daemon/internal/core/services/coldstarter/servers"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ColdStarter struct {
	handler      map[string]ports.ColdStarterServerInterface
	dir          string
	finishTime   *time.Time
	finishOnce   sync.Once
	portsService ports.PortServiceInterface
	finishChan   chan *domain.AugmentedPort
	chandlers    []ports.ColdStarterHandlerInterface
	queueManager ports.QueueManagerInterface
	handlerMu    sync.Mutex
	progress     *domain.SnapshotProgress
}

func NewColdStarter(
	portsService ports.PortServiceInterface,
	queueManager ports.QueueManagerInterface,
	dir string,
) *ColdStarter {
	return &ColdStarter{
		handler:      make(map[string]ports.ColdStarterServerInterface),
		dir:          dir,
		finishTime:   nil,
		portsService: portsService,
		finishChan:   make(chan *domain.AugmentedPort),
		chandlers:    nil,
		queueManager: queueManager,
		handlerMu:    sync.Mutex{},
		progress:     domain.NewSnapshotProgress(),
	}
}

func (c *ColdStarter) Start(ctx context.Context) chan *domain.AugmentedPort {
	c.finishChan = make(chan *domain.AugmentedPort)

	go c.Serve(ctx)

	return c.finishChan
}

func (c *ColdStarter) Serve(ctx context.Context) {
	augmentedPorts := c.portsService.GetPorts()

	augmentedPortMap := make(map[string]int, len(augmentedPorts))
	for _, p := range augmentedPorts {
		augmentedPortMap[p.Name] = p.Port.Port
	}

	// Initialize the handler map with a length of augmentedPorts
	c.handler = make(map[string]ports.ColdStarterServerInterface, len(augmentedPorts))

	for _, port := range augmentedPorts {
		port := port
		if port.ColdstarterHandler == "" {
			logger.Log().Warn("Skipping coldstarter port without handler", zap.Int("port", port.Port.Port), zap.String("port_name", port.Name))
			continue
		}

		var handler ports.ColdStarterHandlerInterface
		if port.ColdstarterHandler == "generic" {
			handler = lua.NewGenericReturnHandler()
		} else {
			path := filepath.Join(c.dir, filepath.Clean(port.ColdstarterHandler))
			if rel, err := filepath.Rel(c.dir, path); err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") {
				logger.Log().Error("Invalid coldstarter handler path", zap.String("handler", port.ColdstarterHandler))
				continue
			}
			handler = lua.NewLuaHandler(c.queueManager, path, c.dir, port.ColdstarterVars, augmentedPortMap, c.progress)
		}

		c.chandlers = append(c.chandlers, handler)

		finishFunc := func() {
			c.Finish(port)
		}

		var server ports.ColdStarterServerInterface
		switch port.Protocol {
		case "udp":
			logger.Log().Info("Starting UDP coldstarter", zap.Int("port", port.Port.Port), zap.String("handler", port.ColdstarterHandler), zap.String("port_name", port.Name))
			server = servers.NewUDP(handler)
		case "tcp", "http", "https", "":
			logger.Log().Info("Starting TCP coldstarter", zap.Int("port", port.Port.Port), zap.String("handler", port.ColdstarterHandler), zap.String("port_name", port.Name))
			server = servers.NewTCP(handler)
		default:
			logger.Log().Warn("Unsupported coldstarter protocol", zap.String("protocol", port.Protocol), zap.String("port_name", port.Name))
			continue
		}
		if err := server.Start(port.Port.Port, finishFunc); err != nil {
			logger.Log().Error("Failed to start coldstarter listener", zap.Error(err), zap.String("port_name", port.Name), zap.Int("port", port.Port.Port))
			continue
		}
		c.handlerMu.Lock()
		c.handler[port.Name] = server
		c.handlerMu.Unlock()

		srv := server
		go func() {
			<-ctx.Done()
			if err := srv.Close(); err != nil {
				logger.Log().Warn("Failed to close coldstarter listener", zap.Error(err))
			}
		}()
	}

}

func (c *ColdStarter) Stop() {
	logger.Log().Info("Stopping ColdStarter")

	for _, handler := range c.handler {
		err := handler.Close()
		if err != nil {
			logger.Log().Error("Error closing handler", zap.Error(err))
		}
	}
}

func (c *ColdStarter) Finish(port *domain.AugmentedPort) {
	c.finishOnce.Do(func() {
		now := time.Now()
		c.finishTime = &now
		for _, handler := range c.chandlers {
			handler.SetFinishedAt(c.finishTime)
		}
		if port == nil {
			logger.Log().Info("Received coldstarter finish signal")
		} else {
			logger.Log().Info("Coldstarter port finished", zap.Int("port", port.Port.Port), zap.String("port_name", port.Name))
		}
		c.finishChan <- port
	})
}
