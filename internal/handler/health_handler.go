package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type HealthHandler struct {
	portService     ports.PortServiceInterface
	timeoutDone     bool
	Started         *time.Time
	snapshotService ports.SnapshotService
}

func NewHealthHandler(
	portService ports.PortServiceInterface,
	timeoutSec uint,
	snapshotService ports.SnapshotService,
) *HealthHandler {

	h := &HealthHandler{
		portService,
		false,
		nil,
		snapshotService,
	}

	// if timeoutSec == 0, we want at some point to not show a bad health status
	if timeoutSec != 0 {
		timeout := time.NewTimer(time.Duration(timeoutSec) * time.Second)
		go h.countdown(timeout)
	}

	return h
}

func (p *HealthHandler) GetHealthAuth(c *fiber.Ctx) error {

	portsOpen := p.portService.MandatoryPortsOpen()

	if !p.timeoutDone && !portsOpen {
		c.SendStatus(503)
		return c.JSON(api.HealthResponse{
			Mode: "manditory_ports",
		})

	}
	if p.Started == nil {
		return c.JSON(api.HealthResponse{
			Mode: "idle",
		})
	}

	if p.snapshotService.GetCurrentMode() != ports.SnapshotModeNoop {
		pt := p.snapshotService.GetCurrentProgressTracker()
		var perc float64
		if pt != nil {
			perc = (*pt).GetPercent()
		}

		return c.JSON(api.HealthResponse{
			Mode:     string(p.snapshotService.GetCurrentMode()),
			Progress: float32(perc),
		})
	}

	return c.JSON(api.HealthResponse{
		Mode:      "ok",
		StartDate: p.Started,
	})
}

func (p *HealthHandler) countdown(timeout *time.Timer) {
	<-timeout.C
	p.timeoutDone = true
}
