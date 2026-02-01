package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ScrollMetricHandler struct {
	ScrollService  ports.ScrollServiceInterface
	ProcessMonitor ports.ProcessMonitorInterface
}

func NewScrollMetricHandler(scrollService ports.ScrollServiceInterface, processMonitor ports.ProcessMonitorInterface) *ScrollMetricHandler {
	return &ScrollMetricHandler{ScrollService: scrollService, ProcessMonitor: processMonitor}
}

// Keep original type aliases (use pointers to match service return types)
type PsTress = map[string]*domain.ProcessTreeRoot
type Metrics = map[string]*domain.ProcessMonitorMetrics

func (sl ScrollMetricHandler) GetMetrics(c *fiber.Ctx) error {
	return c.JSON(sl.ProcessMonitor.GetAllProcessesMetrics())
}

func (sl ScrollMetricHandler) GetPsTree(c *fiber.Ctx) error {
	return c.JSON(sl.ProcessMonitor.GetPsTrees())
}
