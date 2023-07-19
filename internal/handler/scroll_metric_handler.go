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

type PsTress = map[string]*domain.ProcessTreeRoot // @name PsTreeMap

type Metrics = map[string]*domain.ProcessMonitorMetrics // @name ProcessMonitorMetricsMap

// Metrics returns the metrics for all processes.
//
// @Summary Get all process metrics
// @Description Get the metrics for all processes.
// @Tags metrics, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} Metrics
// @Router /api/v1/metrics [get]
func (sl ScrollMetricHandler) Metrics(c *fiber.Ctx) error {
	return c.JSON(sl.ProcessMonitor.GetAllProcessesMetrics())
}

// Returns whole PSTree of process
//
// @Summary Get all process metrics
// @Description Get pstree of running process
// @Tags metrics, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} PsTress
// @Router /api/v1/pstree [get]
func (sl ScrollMetricHandler) PsTree(c *fiber.Ctx) error {
	return c.JSON(sl.ProcessMonitor.GetPsTrees())
}
